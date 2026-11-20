package namebase

import (
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
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Сегмент: magic, блоки gzip, оглавление gzip, хвост.
const (
	magic      = "TIMNAM01"
	footerSize = 56      // смещение и длина оглавления по 8 байт, sha256 оглавления, magic
	frontMode  = "front" // запись блока: байт длины общего начала с прошлым именем, остаток, \n
	blockSize  = 256 << 10
)

// indexHead: первая строка оглавления.
type indexHead struct {
	Mode  string `json:"mode"`
	Block int    `json:"block"`
	Names int64  `json:"names"`
}

type blockRef struct {
	key   string // первое имя блока
	off   int64
	size  int64
	names int
}

// segFile: открытый сегмент. Имена в нём задом наперёд (com.example.www) и по порядку байтов.
type segFile struct {
	f      *os.File
	blocks []blockRef
	last   string // последнее имя сегмента, если оно известно
}

func openSegment(path, last string) (*segFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	s := &segFile{f: f, last: last}
	if err := s.loadIndex(); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("сегмент %s: %w", filepath.Base(path), err)
	}
	return s, nil
}

func (s *segFile) close() { _ = s.f.Close() }

func (s *segFile) loadIndex() error {
	st, err := s.f.Stat()
	if err != nil {
		return err
	}
	total := st.Size()
	if total < int64(len(magic)+footerSize) {
		return errors.New("файл обрезан")
	}
	var foot [footerSize]byte
	if _, err := s.f.ReadAt(foot[:], total-footerSize); err != nil {
		return err
	}
	if string(foot[48:]) != magic {
		return errors.New("это не сегмент архива имён")
	}
	off, size := int64(binary.LittleEndian.Uint64(foot[0:])), int64(binary.LittleEndian.Uint64(foot[8:]))
	if off < int64(len(magic)) || size <= 0 || off > total || size > total || off+size+footerSize != total {
		return errors.New("файл обрезан")
	}
	packed := make([]byte, size)
	if _, err := s.f.ReadAt(packed, off); err != nil {
		return err
	}
	if sum := sha256.Sum256(packed); !bytes.Equal(sum[:], foot[16:48]) {
		return errors.New("оглавление повреждено")
	}
	text, err := gunzip(packed)
	if err != nil {
		return fmt.Errorf("оглавление не читается: %w", err)
	}
	s.blocks, err = parseIndex(string(text), off)
	return err
}

// scan добавляет в res имя q.rev и всё под ним; имена не больше after не считаются.
func (s *segFile) scan(q query, after string, res *found) error {
	self, start := s.blockOf(q.rev), max(s.blockOf(q.pre), s.blockOf(after), 0)
	// между именем и его поддоменами стоят соседи с дефисом, поэтому само имя бывает в более раннем блоке
	if self >= 0 && self < start && q.rev > after {
		if err := s.take(self, q, after, res); err != nil {
			return err
		}
	}
	for k := start; k < len(s.blocks) && s.blocks[k].key < q.end; k++ {
		// блок целиком под доменом: когда выдача полна, хватает счёта из оглавления
		b := s.blocks[k]
		if res.full() && b.key >= q.pre && b.key > after && s.below(k, q.end) {
			res.total += b.names
			continue
		}
		if err := s.take(k, q, after, res); err != nil {
			return err
		}
	}
	return nil
}

// below: все имена блока k меньше end.
func (s *segFile) below(k int, end string) bool {
	if k+1 < len(s.blocks) {
		return s.blocks[k+1].key <= end
	}
	return s.last != "" && s.last < end
}

// blockOf: блок, в котором лежало бы имя; -1 - имя раньше первого блока.
func (s *segFile) blockOf(name string) int {
	return sort.Search(len(s.blocks), func(i int) bool { return s.blocks[i].key > name }) - 1
}

// take читает блок k и берёт из него само имя и его поддомены.
func (s *segFile) take(k int, q query, after string, res *found) error {
	names, err := s.read(k)
	if err != nil {
		return err
	}
	floor := sort.Search(len(names), func(i int) bool { return names[i] > after })
	if i, ok := slices.BinarySearch(names, q.rev); ok && i >= floor {
		res.add(names[i : i+1])
	}
	from, _ := slices.BinarySearch(names, q.pre)
	to, _ := slices.BinarySearch(names, q.end)
	if from = max(from, floor); from < to {
		res.add(names[from:to])
	}
	return nil
}

func (s *segFile) read(k int) ([]string, error) {
	b := s.blocks[k]
	packed := make([]byte, b.size)
	if _, err := s.f.ReadAt(packed, b.off); err != nil {
		return nil, fmt.Errorf("блок %d не читается: %w", k, err)
	}
	raw, err := gunzip(packed)
	if err != nil {
		return nil, fmt.Errorf("блок %d не читается: %w", k, err)
	}
	names, err := decodeBlock(raw, b.names)
	if err != nil {
		return nil, fmt.Errorf("блок %d: %w", k, err)
	}
	return names, nil
}

// parseIndex: в оглавлении строка JSON и по строке "первое имя, смещение, длина, имён" на блок; end - где кончаются блоки.
func parseIndex(text string, end int64) ([]blockRef, error) {
	first, rest, _ := strings.Cut(text, "\n")
	var head indexHead
	if err := json.Unmarshal([]byte(first), &head); err != nil || head.Mode != frontMode {
		return nil, errors.New("оглавление неизвестного вида")
	}
	var blocks []blockRef
	var names int64
	for line := range strings.Lines(rest) {
		f := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
		if len(f) != 4 {
			return nil, errors.New("оглавление повреждено")
		}
		off, err1 := strconv.ParseInt(f[1], 10, 64)
		size, err2 := strconv.ParseInt(f[2], 10, 64)
		count, err3 := strconv.Atoi(f[3])
		parsed := err1 == nil && err2 == nil && err3 == nil && count > 0
		inside := off >= 0 && off <= end && size > 0 && size <= end-off
		sorted := len(blocks) == 0 || blocks[len(blocks)-1].key < f[0]
		if !parsed || !inside || !sorted {
			return nil, errors.New("оглавление повреждено")
		}
		blocks = append(blocks, blockRef{key: f[0], off: off, size: size, names: count})
		names += int64(count)
	}
	if names != head.Names {
		return nil, errors.New("оглавление повреждено")
	}
	return blocks, nil
}

func decodeBlock(raw []byte, count int) ([]string, error) {
	names := make([]string, 0, min(count, len(raw)/2))
	var name []byte
	for len(raw) > 0 {
		same := int(raw[0])
		end := bytes.IndexByte(raw[1:], '\n')
		if same > len(name) || end < 0 {
			return nil, errors.New("блок повреждён")
		}
		name = append(name[:same], raw[1:1+end]...)
		names = append(names, string(name))
		raw = raw[2+end:]
	}
	if len(names) != count {
		return nil, errors.New("блок повреждён")
	}
	return names, nil
}

func gunzip(packed []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(zr)
}
