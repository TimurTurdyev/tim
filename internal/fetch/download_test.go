package fetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// origin: сервер в памяти с Range и If-Range; по сценарию обрывает тело и отвечает кодом вместо файла.
type origin struct {
	data  []byte
	etag  string
	cut   []int    // на запросе i тело обрывается после cut[i] байт; 0 - не обрывается
	codes []int    // на запросе i - этот код вместо файла; 0 - файл
	asked []string // Range каждого запроса
}

func (o *origin) RoundTrip(req *http.Request) (*http.Response, error) {
	i := len(o.asked)
	o.asked = append(o.asked, req.Header.Get("Range"))
	if i < len(o.codes) && o.codes[i] != 0 {
		return answer(req, o.codes[i], http.Header{}, nil), nil
	}
	head := http.Header{"Etag": {o.etag}}
	code, start := http.StatusOK, 0
	if asked := req.Header.Get("Range"); asked != "" && req.Header.Get("If-Range") == o.etag {
		start, _ = strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(asked, "bytes="), "-"))
		if start >= len(o.data) {
			head.Set("Content-Range", fmt.Sprintf("bytes */%d", len(o.data)))
			return answer(req, http.StatusRequestedRangeNotSatisfiable, head, nil), nil
		}
		code = http.StatusPartialContent
		head.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(o.data)-1, len(o.data)))
	}
	resp := answer(req, code, head, o.data[start:])
	if i < len(o.cut) && o.cut[i] > 0 {
		sent := bytes.NewReader(o.data[start : start+o.cut[i]])
		resp.Body = io.NopCloser(io.MultiReader(sent, iotest.ErrReader(io.ErrUnexpectedEOF)))
	}
	return resp, nil
}

func answer(req *http.Request, code int, head http.Header, body []byte) *http.Response {
	return &http.Response{StatusCode: code, Status: strconv.Itoa(code) + " " + http.StatusText(code), Header: head,
		Body: io.NopCloser(bytes.NewReader(body)), ContentLength: int64(len(body)), Request: req}
}

func sample(seed byte, size int) []byte {
	data := make([]byte, size)
	for i := range data {
		data[i] = seed + byte(i%251) + byte(i>>8)
	}
	return data
}

func TestFileResume(t *testing.T) {
	const v0, v1 = `"v0"`, `"v1"`
	data, stale := sample(1, 300_000), sample(7, 300_000)
	sum := sha256.Sum256(data)
	right := hex.EncodeToString(sum[:])
	cases := []resumeCase{
		{name: "с начала", origin: origin{etag: v1}, asked: []string{""}},
		{name: "обрыв посреди тела", origin: origin{etag: v1, cut: []int{100_000}},
			asked: []string{"", "bytes=100000-"}},
		{name: "обрыв, а версию сервер не назвал", origin: origin{cut: []int{100_000}}, asked: []string{"", ""}},
		{name: "докачка после прошлого вызова", part: data[:70_000], tag: v1, origin: origin{etag: v1},
			asked: []string{"bytes=70000-"}, resumed: true},
		{name: "версию назвал вызывающий", part: data[:70_000], origin: origin{etag: v1}, opt: Options{ETag: "v1"},
			asked: []string{"bytes=70000-"}, resumed: true},
		{name: "на сервере другая версия", part: stale[:70_000], tag: v0, origin: origin{etag: v1},
			asked: []string{"bytes=70000-"}},
		{name: "версия скачанного неизвестна", part: stale[:70_000], origin: origin{etag: v1}, asked: []string{""}},
		{name: "ждали не ту версию, что скачана", part: stale[:70_000], tag: v0, origin: origin{etag: v1},
			opt: Options{ETag: v1}, asked: []string{""}},
		{name: "скачанное длиннее файла", part: sample(1, 400_000), tag: v1, origin: origin{etag: v1},
			asked: []string{"bytes=400000-", ""}},
		{name: "сервер занят", origin: origin{etag: v1, codes: []int{503, 429}}, asked: []string{"", "", ""}},
		{name: "сумма не сошлась", origin: origin{etag: v1}, opt: Options{SHA256: strings.Repeat("0", 64)},
			asked: []string{""}, fail: true},
		{name: "больше предела", origin: origin{etag: v1}, opt: Options{MaxBytes: 1000}, asked: []string{""},
			fail: true},
		{name: "файла нет", origin: origin{codes: []int{404}}, asked: []string{""}, fail: true},
	}
	for _, c := range cases {
		c.origin.data = data
		if c.opt.SHA256 == "" {
			c.opt.SHA256 = right
		}
		c.check(t)
	}
}

type resumeCase struct {
	name    string
	part    []byte // что осталось от прошлого вызова
	tag     string // запись о его версии
	origin  origin
	opt     Options
	asked   []string
	resumed bool
	fail    bool
}

func (c resumeCase) check(t *testing.T) {
	t.Helper()
	dst := filepath.Join(t.TempDir(), "file.bin")
	if c.part != nil {
		write(t, dst+".part", c.part)
	}
	if c.tag != "" {
		write(t, dst+".part.etag", []byte(c.tag+"\n"))
	}
	d, err := newDownload("https://example.com/file.bin", dst, c.opt)
	if err != nil {
		t.Fatalf("%s: %v", c.name, err)
	}
	d.client = &http.Client{Transport: &c.origin}
	d.sleep = func(context.Context, time.Duration) error { return nil }
	info, err := d.run(context.Background())
	if !slices.Equal(c.origin.asked, c.asked) {
		t.Errorf("%s: запросы %q, ждали %q", c.name, c.origin.asked, c.asked)
	}
	got, _ := os.ReadFile(dst)
	left, _ := filepath.Glob(dst + ".part*")
	switch {
	case c.fail && (err == nil || got != nil):
		t.Errorf("%s: ошибка %v, готовый файл %d байт - ждали ошибку без файла", c.name, err, len(got))
	case c.fail:
	case err != nil:
		t.Errorf("%s: %v", c.name, err)
	case !bytes.Equal(got, c.origin.data) || info.Bytes != int64(len(got)) || info.Resumed != c.resumed:
		t.Errorf("%s: файл совпал: %v, итог %+v", c.name, bytes.Equal(got, c.origin.data), info)
	case len(left) > 0:
		t.Errorf("%s: после загрузки остались %v", c.name, left)
	}
}

func write(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
