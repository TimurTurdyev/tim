package cmd

import (
	"fmt"
	"image"
	_ "image/gif"  // форматы картинок превью
	_ "image/jpeg" // для image.DecodeConfig
	_ "image/png"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/og"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ogClient: ботов мессенджеров часто пускают иначе, чем браузер, поэтому представляемся как они.
var ogClient = &http.Client{Timeout: 15 * time.Second}

const ogAgent = "TelegramBot (like TwitterBot)"

func ogGet(url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", ogAgent)
	return ogClient.Do(req)
}

// checkImage: размер и вес картинки превью.
func checkImage(url string) (string, []og.Problem) {
	resp, err := ogGet(url)
	if err != nil {
		return "", []og.Problem{{Bad: true, Text: "картинка не скачивается: " + ui.ShortErr(err)}}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return "", []og.Problem{{Bad: true, Text: fmt.Sprintf("картинка отвечает %d", resp.StatusCode)}}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
	if err != nil {
		return "", []og.Problem{{Bad: true, Text: "картинка: " + err.Error()}}
	}
	var probs []og.Problem
	// Telegram не показывает картинки тяжелее 5 МБ, Facebook - тяжелее 8 МБ.
	if len(data) > 5<<20 {
		probs = append(probs, og.Problem{Text: "картинка " + ui.Bytes(int64(len(data))) + ": Telegram покажет превью без неё, нужно меньше 5 MB"})
	}
	cfg, format, err := image.DecodeConfig(strings.NewReader(string(data)))
	if err != nil {
		return ui.Bytes(int64(len(data))) + ", " + resp.Header.Get("Content-Type"), probs
	}
	info := fmt.Sprintf("%dx%d %s, %s", cfg.Width, cfg.Height, format, ui.Bytes(int64(len(data))))
	// Меньше 1200x630 соцсети показывают маленькой картинкой сбоку, а не большой сверху.
	if cfg.Width < 1200 || cfg.Height < 630 {
		probs = append(probs, og.Problem{Text: fmt.Sprintf("картинка %dx%d: для большого превью нужно от 1200x630", cfg.Width, cfg.Height)})
	}
	return info, probs
}

var ogCmd = &cobra.Command{
	Use:   "og <url>",
	Short: "Как ссылка будет выглядеть в Telegram и соцсетях: заголовок, описание, картинка",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		if !strings.Contains(url, "://") {
			url = "https://" + url
		}
		resp, err := ogGet(url)
		if err != nil {
			return ui.Errorf("%s: %v", url, ui.ShortErr(err))
		}
		card := og.Parse(io.LimitReader(resp.Body, 2<<20))
		_ = resp.Body.Close()
		if resp.StatusCode != 200 {
			ui.Println(ui.Warn(fmt.Sprintf("страница отвечает %d боту мессенджера - превью может не собраться", resp.StatusCode)))
		}
		show := func(label, v, fallback string) {
			switch {
			case v != "":
				ui.Println(ui.Label(ui.Pad(label, 11)) + v)
			case fallback != "":
				ui.Println(ui.Label(ui.Pad(label, 11)) + ui.Dim(fallback+"  (запасной вариант)"))
			default:
				ui.Println(ui.Label(ui.Pad(label, 11)) + ui.Dim("-"))
			}
		}
		show("сайт", card.SiteName, "")
		show("заголовок", card.Title, card.HTMLTitle)
		show("описание", card.Description, card.MetaDescription)
		probs := og.Check(card)
		if card.Image != "" {
			img := og.ImageURL(url, card)
			info, more := checkImage(img)
			show("картинка", img, "")
			if info != "" {
				ui.Println(ui.Pad("", 11) + ui.Dim(info))
			}
			probs = append(probs, more...)
		}
		if len(probs) == 0 {
			ui.Println(ui.Label("превью в порядке"))
			return nil
		}
		ui.Println()
		bad := 0
		for _, p := range probs {
			if p.Bad {
				bad++
				ui.Println(ui.Bad(p.Text))
			} else {
				ui.Println(ui.Warn(p.Text))
			}
		}
		ui.Println(ui.Dim("Telegram кеширует превью: после правки отправь ссылку @WebpageBot, чтобы обновить"))
		if bad > 0 {
			return ui.Errorf("превью соберётся плохо")
		}
		return nil
	},
}

func init() {
	root.AddCommand(ogCmd)
}
