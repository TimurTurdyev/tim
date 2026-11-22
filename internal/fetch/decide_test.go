package fetch

import (
	"testing"
	"time"
)

func TestDecide(t *testing.T) {
	const tag, whole, tail = `"v1"`, "bytes 0-499/500", "bytes 100-499/500"
	restart, fail, wait := verdict{step: stepRestart}, verdict{step: stepFail}, verdict{step: stepWait}
	cases := []struct {
		name  string
		reply reply
		have  int64
		want  verdict
	}{
		{"файл с начала", reply{code: 200, length: 500, etag: tag}, 0, verdict{stepWrite, 500}},
		{"размер неизвестен", reply{code: 200, length: -1}, 0, verdict{stepWrite, 0}},
		{"200 при докачке", reply{code: 200, length: 500, etag: `"v2"`}, 100, verdict{stepWrite, 500}},
		{"206 с верным смещением", reply{code: 206, contentRange: tail, etag: tag}, 100, verdict{stepAppend, 500}},
		{"206 без ETag и размера", reply{code: 206, contentRange: "bytes 100-499/*"}, 100, verdict{stepAppend, 0}},
		{"206 с чужим смещением", reply{code: 206, contentRange: whole, etag: tag}, 100, restart},
		{"206 другой версии", reply{code: 206, contentRange: tail, etag: `"v2"`}, 100, restart},
		{"206 со слабым ETag", reply{code: 206, contentRange: tail, etag: `W/"v1"`}, 100, restart},
		{"206 без Content-Range", reply{code: 206, etag: tag}, 100, restart},
		{"206 на запрос без Range", reply{code: 206, contentRange: whole}, 0, fail},
		{"416", reply{code: 416, contentRange: "bytes */500"}, 600, restart},
		{"416 на запрос без Range", reply{code: 416}, 0, fail},
		{"503", reply{code: 503}, 100, wait},
		{"429", reply{code: 429}, 0, wait},
		{"404", reply{code: 404}, 100, fail},
	}
	for _, c := range cases {
		if got := decide(c.reply, c.have, tag); got != c.want {
			t.Errorf("%s: %+v, ждали %+v", c.name, got, c.want)
		}
	}
}

func TestParseContentRange(t *testing.T) {
	cases := []struct {
		in           string
		start, total int64
		ok           bool
	}{
		{"bytes 100-199/200", 100, 200, true},
		{"bytes 0-0/1", 0, 1, true},
		{"bytes 100-199/*", 100, 0, true},
		{"bytes 5-2/10", 0, 0, false},
		{"bytes 0-9/5", 0, 0, false},
		{"bytes -5-9/20", 0, 0, false},
		{"bytes */500", 0, 0, false},
		{"bytes 0-9", 0, 0, false},
		{"items 0-9/10", 0, 0, false},
		{"", 0, 0, false},
	}
	for _, c := range cases {
		start, total, ok := parseContentRange(c.in)
		if start != c.start || total != c.total || ok != c.ok {
			t.Errorf("%q: %d %d %v, ждали %d %d %v", c.in, start, total, ok, c.start, c.total, c.ok)
		}
	}
}

func TestResumeTag(t *testing.T) {
	cases := []struct{ name, want, saved, tag string }{
		{"версия записана", "", `"v1"` + "\n", `"v1"`},
		{"ждут ту же версию", `"v1"`, `"v1"`, `"v1"`},
		{"ждут другую версию", `"v2"`, `"v1"`, ""},
		{"записи нет, версию назвал вызывающий", "v1", "", `"v1"`},
		{"версия неизвестна", "", "", ""},
		{"слабый ETag", "", `W/"v1"`, ""},
		{"запись испорчена", "", "v1\nv2", ""},
	}
	for _, c := range cases {
		if got := resumeTag(c.want, c.saved); got != c.tag {
			t.Errorf("%s: %q, ждали %q", c.name, got, c.tag)
		}
	}
}

func TestBackoff(t *testing.T) {
	cases := []struct {
		n          int
		retryAfter string
		want       time.Duration
	}{
		{0, "", time.Second},
		{3, "", 8 * time.Second},
		{maxWaits - 1, "", 32 * time.Second},
		{0, "5", 5 * time.Second},
		{2, "1", 4 * time.Second},
		{0, "3600", time.Minute},
		{0, "-3", time.Second},
		{0, "Wed, 25 Nov 2026 10:00:00 GMT", time.Second},
	}
	for _, c := range cases {
		if got := backoff(c.n, c.retryAfter); got != c.want {
			t.Errorf("повтор %d, Retry-After %q: %v, ждали %v", c.n, c.retryAfter, got, c.want)
		}
	}
}
