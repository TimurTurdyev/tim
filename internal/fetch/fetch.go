// Package fetch: загрузка по HTTP для больших данных - файл потоком с докачкой и небольшой ответ в память.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	defaultStall = 20 * time.Second
	agent        = "tim"
	// Столько раз ждём занятый сервер и столько раз продолжаем после обрыва.
	maxWaits = 6
	maxDrops = 6
)

var (
	// ErrStalled: данные перестали идти; недокачанное остаётся на диске.
	ErrStalled = errors.New("данные перестали идти")
	// ErrNotModified: ответ 304 на условный запрос в Bytes.
	ErrNotModified = errors.New("на сервере ничего не изменилось")

	// Общего срока нет: большой файл качается долго, а зависший запрос снимает сторож.
	client = &http.Client{}
)

// Options: как качать файл.
type Options struct {
	ETag     string                 // ожидаемый ETag: докачка только того же файла
	SHA256   string                 // ожидаемая сумма готового файла, пусто - не сверять
	MaxBytes int64                  // предел размера, 0 - нет
	Stall    time.Duration          // столько без единого байта - ErrStalled; 0 - 20 с
	Header   http.Header            // добавка к заголовкам запроса
	Progress func(got, total int64) // на каждый записанный кусок; total 0 - размер неизвестен
}

// Info: что скачано; при ошибке - что осталось в .part.
type Info struct {
	ETag    string
	Bytes   int64
	Resumed bool // начало файла осталось от прошлого вызова
}

// File качает url в dst + ".part" и по готовности переименовывает в dst. Если .part остался от той же
// версии файла, докачивает его; версию хранит dst + ".part.etag".
func File(ctx context.Context, url, dst string, opt Options) (Info, error) {
	d, err := newDownload(url, dst, opt)
	if err != nil {
		return Info{}, err
	}
	return d.run(ctx)
}

// Bytes: небольшой ответ целиком в память. Ответ длиннее limit - ошибка, а не обрезка; 304 - ErrNotModified.
func Bytes(ctx context.Context, url string, limit int64, header http.Header) ([]byte, http.Header, error) {
	for waits := 0; ; waits++ {
		data, got, err := get(ctx, url, limit, header)
		var busy busyError
		if errors.As(err, &busy) && waits < maxWaits {
			err = sleep(ctx, backoff(waits, busy.retryAfter))
			if err == nil {
				continue
			}
		}
		if errors.Is(err, ErrStalled) {
			client.CloseIdleConnections()
		}
		if err != nil {
			return nil, got, fmt.Errorf("запрос %s: %w", label(url), err)
		}
		return data, got, nil
	}
}

func get(parent context.Context, url string, limit int64, header http.Header) ([]byte, http.Header, error) {
	ctx, w := watch(parent, defaultStall)
	defer w.stop()
	req, err := request(ctx, url, header)
	if err != nil {
		return nil, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, w.failure(ctx, err)
	}
	defer func() { _ = resp.Body.Close() }()
	ranged := resp.StatusCode == http.StatusPartialContent && req.Header.Get("Range") != ""
	switch {
	case resp.StatusCode == http.StatusNotModified:
		return nil, resp.Header, ErrNotModified
	case resp.StatusCode == http.StatusServiceUnavailable, resp.StatusCode == http.StatusTooManyRequests:
		return nil, nil, busyError{status: resp.Status, retryAfter: resp.Header.Get("Retry-After")}
	case resp.StatusCode != http.StatusOK && !ranged:
		return nil, nil, statusError(resp.Status)
	}
	limit = min(limit, math.MaxInt64-1)
	data, err := io.ReadAll(io.LimitReader(w.reader(resp.Body), limit+1))
	if err != nil {
		return nil, nil, w.failure(ctx, err)
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("ответ длиннее %s", ui.Bytes(limit))
	}
	return data, resp.Header, nil
}
