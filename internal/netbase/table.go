package netbase

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sort"
	"time"
)

// Файл таблицы: заголовок, ключи по возрастанию (big-endian), значения, хвост со строками.
const (
	magic      = "TIMN"
	version    = 1
	headerSize = 28
)

const (
	kindAS byte = iota + 1
	kindCountry
	kindNames
)

const (
	asValLen      = 6 // номер AS, длина сети, флаги
	countryValLen = 2
	nameValLen    = 6 // смещение имени в хвосте, страна регистрации
	flagMOAS      = 1
)

// Таблицы каталога в порядке сборки; номер - место в Tables.all.
const (
	as4 = iota
	as6
	names
	cc4
	cc6
	tableCount
)

var specs = [tableCount]tableSpec{
	as4:   {source: risFile4, file: "as4.tbl", kind: kindAS, keyLen: 4, valLen: asValLen},
	as6:   {source: risFile6, file: "as6.tbl", kind: kindAS, keyLen: 16, valLen: asValLen},
	names: {source: namesFile, file: "asname.tbl", kind: kindNames, keyLen: 4, valLen: nameValLen},
	cc4:   {source: countryFile4, file: "cc4.tbl", kind: kindCountry, keyLen: 4, valLen: countryValLen},
	cc6:   {source: countryFile6, file: "cc6.tbl", kind: kindCountry, keyLen: 16, valLen: countryValLen},
}

var errBadTable = errors.New("файл повреждён или другой версии")

// tableSpec: из какого скачанного файла собирается таблица и как она лежит на диске.
type tableSpec struct {
	source, file   string
	kind           byte
	keyLen, valLen int
}

type header struct {
	count, rows, textLen int
	built                time.Time
}

// table: запись действует от своего ключа до следующего; значение из одних нулей - "здесь пусто".
type table struct {
	keyLen, valLen, count int
	built                 time.Time
	keys, vals, text      []byte
}

// find: номер последней записи с ключом не больше данного; -1 - ключ левее первой.
func (t *table) find(key []byte) int {
	n := t.keyLen
	return sort.Search(t.count, func(i int) bool { return bytes.Compare(t.keys[i*n:(i+1)*n], key) > 0 }) - 1
}

func (t *table) covering(key []byte) ([]byte, bool) {
	i := t.find(key)
	if i < 0 {
		return nil, false
	}
	val := t.vals[i*t.valLen : (i+1)*t.valLen]
	for _, b := range val {
		if b != 0 {
			return val, true
		}
	}
	return nil, false
}

func (t *table) exact(key []byte) ([]byte, bool) {
	i := t.find(key)
	if i < 0 || !bytes.Equal(t.keys[i*t.keyLen:(i+1)*t.keyLen], key) {
		return nil, false
	}
	return t.vals[i*t.valLen : (i+1)*t.valLen], true
}

// line: строка хвоста от смещения до перевода строки.
func (t *table) line(offset uint32) string {
	if int64(offset) >= int64(len(t.text)) {
		return ""
	}
	rest := t.text[offset:]
	if end := bytes.IndexByte(rest, '\n'); end >= 0 {
		rest = rest[:end]
	}
	return string(rest)
}

func parseHeader(data []byte, spec tableSpec) (header, error) {
	if len(data) < headerSize || string(data[:4]) != magic || data[4] != version || data[5] != spec.kind ||
		int(data[6]) != spec.keyLen || int(data[7]) != spec.valLen {
		return header{}, errBadTable
	}
	return header{
		count:   int(binary.BigEndian.Uint32(data[8:])),
		rows:    int(binary.BigEndian.Uint32(data[12:])),
		built:   time.Unix(int64(binary.BigEndian.Uint64(data[16:])), 0),
		textLen: int(binary.BigEndian.Uint32(data[24:])),
	}, nil
}

func readTable(path string, spec tableSpec) (table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return table{}, err
	}
	head, err := parseHeader(data, spec)
	if err != nil {
		return table{}, err
	}
	keysLen, valsLen := int64(head.count)*int64(spec.keyLen), int64(head.count)*int64(spec.valLen)
	if int64(len(data)) != headerSize+keysLen+valsLen+int64(head.textLen) {
		return table{}, errBadTable
	}
	keysEnd := headerSize + int(keysLen)
	valsEnd := keysEnd + int(valsLen)
	return table{
		keyLen: spec.keyLen, valLen: spec.valLen, count: head.count, built: head.built,
		keys: data[headerSize:keysEnd], vals: data[keysEnd:valsEnd], text: data[valsEnd:],
	}, nil
}

// rowsBefore: сколько строк источника дошло до прежней таблицы; нет её или она не читается - 0.
func rowsBefore(path string, spec tableSpec) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, headerSize)
	if _, err := io.ReadFull(f, data); err != nil {
		return 0
	}
	head, err := parseHeader(data, spec)
	if err != nil {
		return 0
	}
	return head.rows
}

// writeTemp пишет таблицу во временный файл рядом с её местом; имя отдаёт и при ошибке, чтобы файл убрали.
func writeTemp(dir string, spec tableSpec, sec section, built time.Time) (string, int64, error) {
	head := make([]byte, headerSize)
	copy(head, magic)
	head[4], head[5], head[6], head[7] = version, spec.kind, byte(spec.keyLen), byte(spec.valLen)
	binary.BigEndian.PutUint32(head[8:], uint32(len(sec.keys)/spec.keyLen))
	binary.BigEndian.PutUint32(head[12:], uint32(sec.rows))
	binary.BigEndian.PutUint64(head[16:], uint64(built.Unix()))
	binary.BigEndian.PutUint32(head[24:], uint32(len(sec.text)))
	tmp, err := os.CreateTemp(dir, "."+spec.file+".*")
	if err != nil {
		return "", 0, err
	}
	var size int64
	for _, part := range [][]byte{head, sec.keys, sec.vals, sec.text} {
		if err == nil {
			_, err = tmp.Write(part)
		}
		size += int64(len(part))
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	return tmp.Name(), size, err
}
