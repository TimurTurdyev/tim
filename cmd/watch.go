package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/watch"
)

var (
	watchEvery   time.Duration
	watchTimeout time.Duration
	watchQuiet   bool
)

// probeURL: работает - ответил кодом ниже 500 за отведённое время.
func probeURL(client *http.Client, url string) (bool, string) {
	start := time.Now()
	resp, err := client.Get(url)
	took := time.Since(start).Round(time.Millisecond)
	if err != nil {
		return false, ui.ShortErr(err)
	}
	// Дочитать тело, чтобы соединение вернулось в пул: иначе каждый опрос - новый TLS.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return resp.StatusCode < 500, fmt.Sprintf("%d за %v", resp.StatusCode, took)
}

var watchCmd = &cobra.Command{
	Use:   "watch <url> [--every 10s] [--timeout 10s]",
	Short: "Следить за сайтом и сообщить, когда он упал и когда поднялся",
	Long: `Опрашивает адрес раз в --every. Падение - две неудачи подряд (ошибка соединения или код 5xx),
чтобы одно моргание сети не будило зря. На каждой смене состояния - строка и уведомление системы.
Удобно во время деплоя или переезда. Ctrl-C останавливает и печатает итог.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		if !strings.Contains(url, "://") {
			url = "https://" + url
		}
		client := &http.Client{Timeout: watchTimeout}
		var tr watch.Tracker
		probes, fails := 0, 0
		start := time.Now()
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt)
		ticker := time.NewTicker(watchEvery)
		defer ticker.Stop()
		ui.Println(ui.Dim("слежу за " + url + " раз в " + watchEvery.String() + "; Ctrl-C - выход"))
		for {
			ok, detail := probeURL(client, url)
			probes++
			if !ok {
				fails++
			}
			if ev, changed := tr.Observe(ok, time.Now()); changed {
				line := ev.At.Format("15:04:05") + "  "
				switch ev.To {
				case watch.Up:
					line += ui.Label("работает") + "  " + detail
					if ev.From == watch.Down {
						line += ui.Dim("  лежал " + ui.Duration(ev.Was))
						if !watchQuiet {
							notify("tim watch: поднялся", url+" после "+ui.Duration(ev.Was))
						}
					}
				case watch.Down:
					line += ui.Bad("лежит") + "  " + detail
					if !watchQuiet {
						notify("tim watch: лежит", url+": "+detail)
					}
				}
				ui.Println(line)
			}
			select {
			case <-stop:
				ui.Println()
				ui.Println(ui.Dim(fmt.Sprintf("опросов %d, неудачных %d за %s", probes, fails, ui.Duration(time.Since(start)))))
				return nil
			case <-ticker.C:
			}
		}
	},
}

func init() {
	watchCmd.Flags().DurationVar(&watchEvery, "every", 10*time.Second, "как часто опрашивать")
	watchCmd.Flags().DurationVar(&watchTimeout, "timeout", 10*time.Second, "сколько ждать ответа")
	watchCmd.Flags().BoolVar(&watchQuiet, "quiet", false, "без уведомлений системы")
	root.AddCommand(watchCmd)
}
