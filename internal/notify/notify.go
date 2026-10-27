// Package notify: уведомление наружу - Telegram и ntfy простым HTTP, без зависимостей. Токен и тема -
// только в конфиге пользователя.
package notify

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
)

var client = &http.Client{Timeout: 15 * time.Second}

// Configured: куда слать - "Telegram", "ntfy"; пусто - никуда.
func Configured(n config.Notify) []string {
	var out []string
	if n.Telegram != "" && n.Chat != "" {
		out = append(out, "Telegram")
	}
	if n.Ntfy != "" {
		out = append(out, "ntfy")
	}
	return out
}

// Send: одно сообщение во все настроенные места; ошибка - первая, остальные места всё равно пробуются.
func Send(n config.Notify, title, text string) error {
	var first error
	keep := func(err error) {
		if err != nil && first == nil {
			first = err
		}
	}
	if n.Telegram != "" && n.Chat != "" {
		keep(post("https://api.telegram.org/bot"+n.Telegram+"/sendMessage", "application/x-www-form-urlencoded",
			url.Values{"chat_id": {n.Chat}, "text": {title + "\n" + text}, "disable_web_page_preview": {"true"}}.Encode()))
	}
	if n.Ntfy != "" {
		// Заголовок - параметром адреса: в HTTP-заголовке не-ASCII ntfy понимает не всегда.
		sep := "?"
		if strings.Contains(n.Ntfy, "?") {
			sep = "&"
		}
		keep(post(n.Ntfy+sep+"title="+url.QueryEscape(title), "text/plain; charset=utf-8", text))
	}
	return first
}

func post(u, contentType, body string) error {
	req, err := http.NewRequest(http.MethodPost, u, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := client.Do(req)
	if err != nil {
		return redact(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s: %s %s", host(u), resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// redact: в ошибке net/http есть адрес целиком, а в адресе Telegram - токен.
func redact(err error) error {
	if ue, ok := errors.AsType[*url.Error](err); ok {
		return fmt.Errorf("%s: %v", host(ue.URL), ue.Err)
	}
	return err
}

func host(u string) string {
	if p, err := url.Parse(u); err == nil {
		return p.Host
	}
	return "адрес"
}
