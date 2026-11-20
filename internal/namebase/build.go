package namebase

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	readBuffer = 1 << 20
	maxName    = 253
	maxLabel   = 63
	maxSame    = 255 // общее начало с прошлым именем хранится одним байтом
	tmpPrefix  = ".seg-"
	tmpSuffix  = ".tmp"
)

// segWriter пишет сегмент блок за блоком: в памяти только текущий блок и оглавление.
type segWriter struct {
	out    *bufio.Writer
	block  int // сколько байт имён идёт в блок до сжатия
	off    int64
	index  bytes.Buffer
	raw    []byte
	prev   []byte
	first  string
	plain  int
	count  int
	packed bytes.Buffer
	zw     *gzip.Writer
	seg    segment
}

func newSegWriter(out io.Writer, block int) *segWriter {
	w := &segWriter{out: bufio.NewWriterSize(out, readBuffer), block: block}
	w.zw = gzip.NewWriter(&w.packed)
	w.write([]byte(magic))
	return w
}

// write: ошибку записи bufio помнит сам и отдаёт на Flush.
func (w *segWriter) write(p []byte) {
	n, _ := w.out.Write(p)
	w.off += int64(n)
}

func (w *segWriter) add(name []byte) error {
	if w.count == 0 {
		w.first = string(name)
	}
	same := 0
	for same < len(w.prev) && same < len(name) && same < maxSame && w.prev[same] == name[same] {
		same++
	}
	w.raw = append(w.raw, byte(same))
	w.raw = append(w.raw, name[same:]...)
	w.raw = append(w.raw, '\n')
	w.prev = append(w.prev[:0], name...)
	w.plain += len(name) + 1
	w.count++
	if w.plain < w.block {
		return nil
	}
	return w.flush()
}

func (w *segWriter) flush() error {
	if w.count == 0 {
		return nil
	}
	w.packed.Reset()
	w.zw.Reset(&w.packed)
	if _, err := w.zw.Write(w.raw); err != nil {
		return err
	}
	if err := w.zw.Close(); err != nil {
		return err
	}
	fmt.Fprintf(&w.index, "%s\t%d\t%d\t%d\n", w.first, w.off, w.packed.Len(), w.count)
	if w.seg.Names == 0 {
		w.seg.First = w.first
	}
	w.seg.Last = string(w.prev)
	w.seg.Names += int64(w.count)
	w.write(w.packed.Bytes())
	w.raw, w.prev, w.plain, w.count = w.raw[:0], w.prev[:0], 0, 0
	return nil
}

// finish дописывает последний блок, оглавление и хвост.
func (w *segWriter) finish() error {
	if err := w.flush(); err != nil {
		return err
	}
	head, err := json.Marshal(indexHead{Mode: frontMode, Block: w.block, Names: w.seg.Names})
	if err != nil {
		return err
	}
	var packed bytes.Buffer
	zw, err := gzip.NewWriterLevel(&packed, gzip.BestCompression)
	if err != nil {
		return err
	}
	if _, err := zw.Write(append(append(head, '\n'), w.index.Bytes()...)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	var foot [footerSize]byte
	binary.LittleEndian.PutUint64(foot[0:], uint64(w.off))
	binary.LittleEndian.PutUint64(foot[8:], uint64(packed.Len()))
	sum := sha256.Sum256(packed.Bytes())
	copy(foot[16:48], sum[:])
	copy(foot[48:], magic)
	w.write(packed.Bytes())
	w.write(foot[:])
	w.seg.Bytes = w.off
	return w.out.Flush()
}

// buildSegment: часть исходника -> сегмент за один проход; after - последнее имя прошлой части. При ошибке и без годных имён файла нет.
func buildSegment(path string, src io.Reader, etag, after string, block int) (segment, error) {
	tmp, err := os.CreateTemp(filepath.Dir(path), tmpPrefix+"*"+tmpSuffix)
	if err != nil {
		return segment{}, err
	}
	placed := false
	defer func() {
		if !placed {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	sum := newETagSum(etag)
	w := newSegWriter(tmp, block)
	lines, err := copyNames(io.TeeReader(src, sum), after, w)
	if err != nil {
		return segment{}, err
	}
	if err := w.finish(); err != nil {
		return segment{}, fmt.Errorf("сегмент не записался: %w", err)
	}
	seg := w.seg
	seg.Source = lines
	if sum.matches() {
		seg.ETag = sum.want
	}
	if seg.Names == 0 {
		return segment{Source: lines, ETag: seg.ETag}, nil
	}
	seg.File = filepath.Base(path)
	if err := place(tmp, path); err != nil {
		return segment{}, fmt.Errorf("сегмент не записался: %w", err)
	}
	placed = true
	return seg, nil
}

func place(tmp *os.File, path string) error {
	if err := tmp.Chmod(0o644); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// copyNames сверяет порядок строк "номер, табуляция, имя" и пишет годные имена в сегмент; в ответе - сколько строк в части.
func copyNames(src io.Reader, after string, w *segWriter) (int64, error) {
	zr, err := gzip.NewReader(bufio.NewReaderSize(src, readBuffer))
	if err != nil {
		return 0, fmt.Errorf("часть не читается: %w", err)
	}
	lines := bufio.NewReaderSize(zr, readBuffer)
	last := []byte(after)
	var count int64
	for {
		line, rerr := lines.ReadSlice('\n')
		// при обрыве gzip сначала приходит обрезанная строка и только потом ошибка
		if rerr != nil && !errors.Is(rerr, io.EOF) {
			return count, fmt.Errorf("часть не дочитана: %w", rerr)
		}
		if len(line) > 0 {
			name, ok := sourceName(line)
			if !ok {
				return count, fmt.Errorf("строка без табуляции: %s", plain(line))
			}
			if bytes.Compare(name, last) <= 0 {
				return count, fmt.Errorf("имена не по порядку: %s после %s", plain(name), plain(last))
			}
			last = append(last[:0], name...)
			count++
			if goodName(name) {
				if err := w.add(name); err != nil {
					return count, fmt.Errorf("сегмент не записался: %w", err)
				}
			}
		}
		if rerr != nil {
			return count, nil
		}
	}
}

func sourceName(line []byte) ([]byte, bool) {
	_, name, ok := bytes.Cut(bytes.TrimRight(line, "\r\n"), []byte{'\t'})
	return name, ok
}

// goodName: имя (задом наперёд) годится в архив. Подчёркивание и punycode остаются: мусор по смыслу отсеет DNS.
func goodName(name []byte) bool {
	if len(name) > maxName {
		return false
	}
	labels, start, word := 1, 0, false
	for i, c := range name {
		switch {
		case c == '.':
			if !goodLabel(name[start:i]) {
				return false
			}
			labels++
			start = i + 1
		case c >= 'a' && c <= 'z', c == '-', c == '_':
			word = true
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	// из одних цифр - адрес, из одной метки - не имя хоста
	return word && labels > 1 && goodLabel(name[start:])
}

func goodLabel(label []byte) bool {
	return len(label) > 0 && len(label) <= maxLabel && label[0] != '-' && label[len(label)-1] != '-'
}

// plain: чужая строка для текста ошибки.
func plain(b []byte) string {
	return ui.Clip(ui.Plain(string(b)), 80)
}
