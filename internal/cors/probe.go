package cors

import (
	"net/http"
	"strings"
	"time"
)

// Response: что сервер ответил на preflight (если он был) и на сам запрос.
type Response struct {
	Preflight       http.Header // nil - preflight не понадобился
	PreflightCode   int
	PreflightStatus string
	Method, Status  string
	Actual          http.Header
}

// Probe делает то же, что браузер: preflight OPTIONS, если он нужен, и сам запрос с Origin.
// Сам запрос идёт безопасным методом: заголовки ответа проверяются, данные на сервере не меняются.
func Probe(url string, r Request) (Response, error) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	var out Response
	if NeedsPreflight(r) {
		req, err := http.NewRequest(http.MethodOptions, url, nil)
		if err != nil {
			return out, err
		}
		req.Header.Set("Origin", r.Origin)
		req.Header.Set("Access-Control-Request-Method", r.Method)
		if len(r.Headers) > 0 {
			req.Header.Set("Access-Control-Request-Headers", strings.ToLower(strings.Join(r.Headers, ", ")))
		}
		resp, err := client.Do(req)
		if err != nil {
			return out, &StepError{Step: "preflight", Err: err}
		}
		_ = resp.Body.Close()
		out.Preflight, out.PreflightCode, out.PreflightStatus = resp.Header, resp.StatusCode, resp.Status
	}
	out.Method = r.Method
	if out.Method != http.MethodGet && out.Method != http.MethodHead {
		out.Method = http.MethodHead
	}
	req, err := http.NewRequest(out.Method, url, nil)
	if err != nil {
		return out, err
	}
	req.Header.Set("Origin", r.Origin)
	resp, err := client.Do(req)
	if err != nil {
		return out, &StepError{Step: "запрос", Err: err}
	}
	_ = resp.Body.Close()
	out.Actual, out.Status = resp.Header, resp.Status
	return out, nil
}

// StepError: на каком шаге запрос не дошёл.
type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return e.Step + ": " + e.Err.Error() }
func (e *StepError) Unwrap() error { return e.Err }
