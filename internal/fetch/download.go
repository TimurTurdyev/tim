package fetch

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// errRestart: скачанное стёрто, файл надо спросить целиком.
var errRestart = errors.New("скачанное не подошло к ответу сервера")

// download: одна загрузка файла от первого запроса до переименования.
type download struct {
	url, dst, part string
	opt            Options
	stall          time.Duration
	client         *http.Client
	sleep          func(context.Context, time.Duration) error

	etag   string    // версия, к которой относится .part, в виде для If-Range; пусто - докачивать нельзя
	seen   string    // ETag, как его прислал сервер
	have   int64     // байт в .part
	kept   int64     // из них осталось от прошлого вызова
	total  int64     // полный размер, 0 - неизвестен
	sum    hash.Hash // nil - сумму не сверять
	summed int64     // сколько байт .part уже в сумме
	waits  int
	drops  int
	alive  bool // сервер хоть раз ответил
}

func newDownload(url, dst string, opt Options) (*download, error) {
	d := &download{url: url, dst: dst, part: dst + ".part", opt: opt, stall: opt.Stall, client: client, sleep: sleep}
	if d.stall <= 0 {
		d.stall = defaultStall
	}
	if opt.SHA256 != "" {
		d.sum = sha256.New()
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return nil, fmt.Errorf("каталог для загрузки: %w", err)
	}
	saved, _ := os.ReadFile(d.tagFile())
	d.etag = resumeTag(opt.ETag, string(saved))
	if st, err := os.Stat(d.part); err == nil {
		d.have, d.kept = st.Size(), st.Size()
	}
	return d, nil
}

func (d *download) run(ctx context.Context) (Info, error) {
	err := d.pull(ctx)
	if err == nil {
		err = d.finish()
	}
	if errors.Is(err, ErrStalled) {
		// Замершее соединение HTTP/2 осталось бы в запасе и досталось следующему запросу.
		d.client.CloseIdleConnections()
	}
	if err != nil {
		return d.info(), fmt.Errorf("загрузка %s: %w", label(d.url), err)
	}
	return d.info(), nil
}

// pull повторяет запросы, пока в .part не окажется файл целиком или повторять станет незачем.
func (d *download) pull(ctx context.Context) error {
	for {
		err := d.attempt(ctx)
		if err == nil {
			return nil
		}
		if err = d.retry(ctx, err); err != nil {
			return err
		}
	}
}

// attempt: один запрос; nil - файл в .part целиком.
func (d *download) attempt(parent context.Context) error {
	if d.have > 0 && d.etag == "" {
		if err := d.discard(); err != nil {
			return err
		}
	}
	ctx, w := watch(parent, d.stall)
	defer w.stop()
	req, err := request(ctx, d.url, d.opt.Header)
	if err != nil {
		return err
	}
	// Без сжатия на лету: смещения докачки и сумма считаются по байтам самого файла.
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Del("Range")
	req.Header.Del("If-Range")
	if d.have > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(d.have, 10)+"-")
		req.Header.Set("If-Range", d.etag)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return w.failure(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	d.alive = true
	file, err := d.accept(resp, w)
	if err != nil {
		return err
	}
	if err = d.receive(ctx, w, resp.Body, file); err != nil {
		_ = file.Close()
		return err
	}
	if err = errors.Join(file.Sync(), file.Close()); err != nil {
		return fmt.Errorf("запись на диск: %w", err)
	}
	return nil
}

// accept: по ответу решает судьбу скачанного и открывает .part под тело.
func (d *download) accept(resp *http.Response, w *watchdog) (*os.File, error) {
	r := reply{code: resp.StatusCode, length: resp.ContentLength,
		contentRange: resp.Header.Get("Content-Range"), etag: resp.Header.Get("ETag")}
	v := decide(r, d.have, d.etag)
	switch v.step {
	case stepWait:
		return nil, busyError{status: resp.Status, retryAfter: resp.Header.Get("Retry-After")}
	case stepRestart:
		if err := d.discard(); err != nil {
			return nil, err
		}
		return nil, errRestart
	case stepFail:
		return nil, statusError(resp.Status)
	}
	if d.opt.MaxBytes > 0 && v.total > d.opt.MaxBytes {
		return nil, fmt.Errorf("файл %s больше предела %s", ui.Bytes(v.total), ui.Bytes(d.opt.MaxBytes))
	}
	d.total = v.total
	if r.etag != "" {
		d.seen = r.etag
	}
	if v.step == stepWrite {
		return d.begin(r.etag)
	}
	w.pause()
	if err := d.rehash(); err != nil {
		return nil, err
	}
	w.touch()
	file, err := os.OpenFile(d.part, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return nil, fmt.Errorf("недокачанный файл: %w", err)
	}
	return file, nil
}

// begin: пустой .part и запись о его версии; запись - последней, чтобы обрыв не оставил старые байты под новым ETag.
func (d *download) begin(etag string) (*os.File, error) {
	if err := d.discard(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(d.part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, fmt.Errorf("файл для загрузки: %w", err)
	}
	d.etag = strongTag(etag)
	if d.etag == "" {
		return file, nil
	}
	if err := runner.WriteAtomic(d.tagFile(), []byte(d.etag+"\n"), 0o644); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("запись о версии файла: %w", err)
	}
	return file, nil
}

// receive пишет тело в .part; nil - тело дочитано до конца файла.
func (d *download) receive(ctx context.Context, w *watchdog, body io.Reader, file *os.File) error {
	buf := make([]byte, 128<<10)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			w.touch()
			if failed := d.store(file, buf[:n]); failed != nil {
				return failed
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return w.failure(ctx, fmt.Errorf("связь оборвалась: %w", err))
		}
	}
	if d.have < d.total {
		return dropError{fmt.Errorf("связь оборвалась на %s из %s", ui.Bytes(d.have), ui.Bytes(d.total))}
	}
	return nil
}

func (d *download) store(file *os.File, chunk []byte) error {
	if _, err := file.Write(chunk); err != nil {
		return fmt.Errorf("запись на диск: %w", err)
	}
	if d.sum != nil {
		d.sum.Write(chunk)
		d.summed += int64(len(chunk))
	}
	d.have += int64(len(chunk))
	if d.opt.MaxBytes > 0 && d.have > d.opt.MaxBytes {
		_ = d.discard()
		return fmt.Errorf("файл больше предела %s", ui.Bytes(d.opt.MaxBytes))
	}
	if d.opt.Progress != nil {
		d.opt.Progress(d.have, d.total)
	}
	return nil
}

// rehash: сумма того, что осталось в .part до этого запроса; дальше она считается на лету.
func (d *download) rehash() error {
	if d.sum == nil || d.summed == d.have {
		return nil
	}
	file, err := os.Open(d.part)
	if err != nil {
		return fmt.Errorf("недокачанный файл: %w", err)
	}
	defer func() { _ = file.Close() }()
	d.sum.Reset()
	d.summed, err = io.Copy(d.sum, io.LimitReader(file, d.have))
	if err == nil && d.summed != d.have {
		err = io.ErrUnexpectedEOF
	}
	if err != nil {
		return fmt.Errorf("недокачанный файл: %w", err)
	}
	return nil
}

// retry: nil - запрос стоит повторить, пауза уже выдержана; иначе ошибка, с которой загрузка кончается.
func (d *download) retry(ctx context.Context, err error) error {
	var busy busyError
	var drop dropError
	switch {
	case errors.Is(err, errRestart):
		return nil
	case errors.As(err, &busy) && d.waits < maxWaits:
		d.waits++
		return d.sleep(ctx, backoff(d.waits-1, busy.retryAfter))
	case errors.As(err, &drop) && d.alive && d.drops < maxDrops:
		// Пока сервер ни разу не ответил, повторы только тянут время: сети нет или адрес неверный.
		d.drops++
		return d.sleep(ctx, backoff(d.drops-1, ""))
	}
	return err
}

func (d *download) finish() error {
	if d.sum != nil {
		got := hex.EncodeToString(d.sum.Sum(nil))
		if !strings.EqualFold(got, strings.TrimSpace(d.opt.SHA256)) {
			_ = d.discard()
			return fmt.Errorf("сумма sha256 не сошлась: ждали %s, вышло %s", ui.Plain(d.opt.SHA256), got)
		}
	}
	if err := os.Rename(d.part, d.dst); err != nil {
		return fmt.Errorf("готовый файл не встал на место: %w", err)
	}
	_ = os.Remove(d.tagFile())
	return nil
}

// discard стирает .part и запись о его версии.
func (d *download) discard() error {
	for _, path := range []string{d.tagFile(), d.part} {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("старый недокачанный файл: %w", err)
		}
	}
	d.have, d.kept, d.summed = 0, 0, 0
	if d.sum != nil {
		d.sum.Reset()
	}
	return nil
}

func (d *download) tagFile() string { return d.part + ".etag" }

func (d *download) info() Info {
	etag := d.seen
	if etag == "" {
		etag = d.etag
	}
	return Info{ETag: etag, Bytes: d.have, Resumed: d.kept > 0}
}
