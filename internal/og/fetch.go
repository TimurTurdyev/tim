package og

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // форматы картинок превью
	_ "image/jpeg" // для image.DecodeConfig
	_ "image/png"
	"io"
	"net/http"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// agent: ботов мессенджеров часто пускают иначе, чем браузер, поэтому представляемся как они.
const agent = "TelegramBot (like TwitterBot)"

var client = &http.Client{Timeout: 15 * time.Second}

// Fetch: карточка страницы глазами бота мессенджера и код ответа.
func Fetch(url string) (Card, int, error) {
	resp, err := get(url)
	if err != nil {
		return Card{}, 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	return Parse(io.LimitReader(resp.Body, 2<<20)), resp.StatusCode, nil
}

// CheckImage: размер и вес картинки превью и что с ними не так.
func CheckImage(url string) (string, []Problem) {
	resp, err := get(url)
	if err != nil {
		return "", []Problem{{Bad: true, Text: "картинка не скачивается: " + ui.ShortErr(err)}}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", []Problem{{Bad: true, Text: fmt.Sprintf("картинка отвечает %d", resp.StatusCode)}}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", []Problem{{Bad: true, Text: "картинка: " + err.Error()}}
	}
	var probs []Problem
	// Telegram не показывает картинки тяжелее 5 МБ, Facebook - тяжелее 8 МБ.
	if len(data) > 5<<20 {
		probs = append(probs, Problem{Text: "картинка " + ui.Bytes(int64(len(data))) + ": Telegram покажет превью без неё, нужно меньше 5 MB"})
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return ui.Bytes(int64(len(data))) + ", " + resp.Header.Get("Content-Type"), probs
	}
	info := fmt.Sprintf("%dx%d %s, %s", cfg.Width, cfg.Height, format, ui.Bytes(int64(len(data))))
	// Меньше 1200x630 соцсети показывают маленькой картинкой сбоку, а не большой сверху.
	if cfg.Width < 1200 || cfg.Height < 630 {
		probs = append(probs, Problem{Text: fmt.Sprintf("картинка %dx%d: для большого превью нужно от 1200x630", cfg.Width, cfg.Height)})
	}
	return info, probs
}

func get(url string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", agent)
	return client.Do(req)
}
