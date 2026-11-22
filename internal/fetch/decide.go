package fetch

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	firstPause = time.Second
	maxPause   = time.Minute
)

// step: что делать с ответом сервера.
type step int

const (
	stepFail    step = iota // ответ не годится, повтор не поможет
	stepWrite               // тело - файл целиком: писать с начала
	stepAppend              // тело - продолжение: дописывать
	stepRestart             // то, что уже скачано, не годится: стереть и спросить файл целиком
	stepWait                // сервер занят: пауза и повтор
)

// reply: то в ответе сервера, от чего зависит решение.
type reply struct {
	code         int
	length       int64 // Content-Length; меньше нуля - сервер не сказал
	contentRange string
	etag         string
}

// verdict: решение и полный размер файла по ответу, 0 - неизвестен.
type verdict struct {
	step  step
	total int64
}

// decide: что делать с ответом, когда уже скачано have байт версии etag.
func decide(r reply, have int64, etag string) verdict {
	switch r.code {
	case http.StatusOK:
		// Так же и в ответ на докачку: сервер отдаёт файл целиком, дописать его к скачанному - испортить.
		return verdict{step: stepWrite, total: max(r.length, 0)}
	case http.StatusPartialContent:
		return partial(r, have, etag)
	case http.StatusRequestedRangeNotSatisfiable:
		if have > 0 {
			return verdict{step: stepRestart}
		}
	case http.StatusServiceUnavailable, http.StatusTooManyRequests:
		return verdict{step: stepWait}
	}
	return verdict{step: stepFail}
}

// partial: кусок годится, если начинается там, где кончается скачанное, и версия файла та же.
func partial(r reply, have int64, etag string) verdict {
	start, total, ok := parseContentRange(r.contentRange)
	switch {
	case have == 0:
		return verdict{step: stepFail}
	case !ok || start != have:
		return verdict{step: stepRestart}
	case r.etag != "" && strongTag(r.etag) != etag:
		return verdict{step: stepRestart}
	}
	return verdict{step: stepAppend, total: total}
}

// parseContentRange: "bytes 100-199/200" -> начало куска 100 и полный размер 200; размер "*" - 0.
func parseContentRange(s string) (start, total int64, ok bool) {
	span, size, slash := strings.Cut(strings.TrimPrefix(s, "bytes "), "/")
	first, last, dash := strings.Cut(span, "-")
	if !strings.HasPrefix(s, "bytes ") || !slash || !dash {
		return 0, 0, false
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 {
		return 0, 0, false
	}
	end, err := strconv.ParseInt(last, 10, 64)
	if err != nil || end < start {
		return 0, 0, false
	}
	if size == "*" {
		return start, 0, true
	}
	total, err = strconv.ParseInt(size, 10, 64)
	if err != nil || total <= end {
		return 0, 0, false
	}
	return start, total, true
}

// resumeTag: ETag для докачки: want ждёт вызывающий, saved записан при начале файла; пусто - качать заново.
func resumeTag(want, saved string) string {
	want, saved = strongTag(want), strongTag(saved)
	switch {
	case saved == "":
		return want
	case want != "" && want != saved:
		return ""
	}
	return saved
}

// strongTag: ETag в кавычках для If-Range; слабый (W/"...") и негодный для заголовка - пусто: докачки по нему нет.
func strongTag(etag string) string {
	etag = strings.TrimSpace(etag)
	inner := strings.TrimSuffix(strings.TrimPrefix(etag, `"`), `"`)
	bad := func(r rune) bool { return r <= ' ' || r == '"' || r == 0x7f }
	if inner == "" || strings.HasPrefix(etag, "W/") || strings.ContainsFunc(inner, bad) {
		return ""
	}
	return `"` + inner + `"`
}

// backoff: пауза перед повтором n (с нуля) - 1, 2, 4... секунды, не меньше Retry-After и не больше минуты.
func backoff(n int, retryAfter string) time.Duration {
	wait := firstPause << n
	if sec, err := strconv.Atoi(strings.TrimSpace(retryAfter)); err == nil {
		wait = max(wait, time.Duration(min(sec, 60))*time.Second)
	}
	return min(wait, maxPause)
}
