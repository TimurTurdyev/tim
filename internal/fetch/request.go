package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// busyError: сервер просит подождать (503, 429).
type busyError struct{ status, retryAfter string }

func (e busyError) Error() string { return statusError(e.status).Error() }

// dropError: сеть подвела, а не сервер отказал; повтор может помочь.
type dropError struct{ err error }

func (e dropError) Error() string { return e.err.Error() }
func (e dropError) Unwrap() error { return e.err }

// watchdog снимает запрос, когда данные не идут дольше stall.
type watchdog struct {
	alarm *time.Timer
	stall time.Duration
	stop  func()
}

func watch(parent context.Context, stall time.Duration) (context.Context, *watchdog) {
	ctx, cancel := context.WithCancelCause(parent)
	alarm := time.AfterFunc(stall, func() { cancel(ErrStalled) })
	return ctx, &watchdog{alarm: alarm, stall: stall, stop: func() {
		alarm.Stop()
		cancel(nil)
	}}
}

func (w *watchdog) touch() { w.alarm.Reset(w.stall) }

// pause: работа с диском между ответом и чтением тела - не остановка данных.
func (w *watchdog) pause() { w.alarm.Stop() }

func (w *watchdog) reader(r io.Reader) io.Reader { return pulse{r, w} }

// failure: чем кончился запрос - остановкой данных, отменой снаружи или обрывом сети.
func (w *watchdog) failure(ctx context.Context, err error) error {
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	return dropError{bare(err)}
}

// pulse: каждый пришедший кусок отодвигает сторожа.
type pulse struct {
	r io.Reader
	w *watchdog
}

func (p pulse) Read(buf []byte) (int, error) {
	n, err := p.r.Read(buf)
	if n > 0 {
		p.w.touch()
	}
	return n, err
}

func sleep(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

func request(ctx context.Context, address string, header http.Header) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, fmt.Errorf("адрес не разобран: %w", bare(err))
	}
	if header != nil {
		req.Header = header.Clone()
	}
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", agent)
	}
	return req, nil
}

// label: адрес для сообщений - без пароля и без того, что исполнит терминал.
func label(address string) string {
	if u, err := url.Parse(address); err == nil {
		address = u.Redacted()
	}
	return ui.Plain(address)
}

// bare: ошибка без адреса запроса - он уже стоит в начале сообщения, и в нём может быть пароль.
func bare(err error) error {
	var wrapped *url.Error
	if errors.As(err, &wrapped) {
		return wrapped.Err
	}
	return err
}

func statusError(status string) error {
	return fmt.Errorf("сервер ответил %s", ui.Plain(status))
}
