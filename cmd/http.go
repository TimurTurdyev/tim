package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	httpFollow     bool
	httpAllHeaders bool
)

var keyHeaders = []string{"Server", "Content-Type", "Content-Encoding", "Cache-Control", "Location", "Strict-Transport-Security", "X-Powered-By"}

func ms(d time.Duration) string { return fmt.Sprintf("%d мс", d.Milliseconds()) }

var httpCmd = &cobra.Command{
	Use:   "http <url> [-L] [--all-headers]",
	Short: "Сколько отвечает сайт по фазам: DNS, TCP, TLS, первый байт, загрузка",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		url := args[0]
		if !strings.Contains(url, "://") {
			url = "https://" + url
		}
		for hop := 0; hop < 10; hop++ {
			t, err := inspect.Measure(url)
			if err != nil {
				return ui.Errorf("%s: %v", url, err)
			}
			status := fmt.Sprintf("%d %s", t.Status, t.Proto)
			switch {
			case t.Status >= 500:
				status = ui.Bad(status)
			case t.Status >= 400:
				status = ui.Warn(status)
			default:
				status = ui.Label(status)
			}
			ui.Println(status + "  " + url + ui.Dim("  "+ui.Bytes(t.Size)))
			download := t.All - t.TTFB
			ui.Println(ui.Dim(fmt.Sprintf("  dns %s  tcp %s  tls %s  первый байт %s  загрузка %s", ms(t.DNS), ms(t.Connect), ms(t.TLS), ms(t.TTFB), ms(download))) + "  " + ui.Label("всего "+ms(t.All)))
			names := keyHeaders
			if httpAllHeaders {
				names = nil
				for k := range t.Header {
					names = append(names, k)
				}
				sort.Strings(names)
			}
			for _, k := range names {
				if v := t.Header.Get(k); v != "" {
					ui.Println("  " + ui.Pad(k, 26) + " " + v)
				}
			}
			if !httpFollow || t.Location == "" {
				if t.Location != "" {
					ui.Println(ui.Dim("редирект: tim http -L " + args[0]))
				}
				return nil
			}
			url = t.Location
		}
		return ui.Errorf("больше 10 редиректов")
	},
}

func init() {
	httpCmd.Flags().BoolVarP(&httpFollow, "location", "L", false, "идти по редиректам")
	httpCmd.Flags().BoolVar(&httpAllHeaders, "all-headers", false, "все заголовки ответа")
	root.AddCommand(httpCmd)
}
