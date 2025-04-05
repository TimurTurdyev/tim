package cmd

import (
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/cors"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	corsOrigin  string
	corsMethod  string
	corsHeaders []string
	corsCreds   bool
)

var corsCmd = &cobra.Command{
	Use:   "cors <url> --origin https://app.example.com [--method POST] [--header Authorization] [--credentials]",
	Short: "Пустит ли браузер запрос с этого origin, а если нет - чего именно не хватает",
	Long: `Делает то же, что браузер: preflight OPTIONS, если он нужен, и сам запрос с заголовком Origin.
Сравнивает ответы с тем, что собирается сделать фронтенд, и говорит, какой заголовок мешает.
Тело запроса не отправляется: для разбора CORS хватает заголовков.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if corsOrigin == "" {
			return ui.Errorf("откуда запрос: --origin https://app.example.com")
		}
		url := args[0]
		if !strings.Contains(url, "://") {
			url = "https://" + url
		}
		r := cors.Request{Origin: strings.TrimSuffix(corsOrigin, "/"), Method: strings.ToUpper(corsMethod), Headers: corsHeaders, Credentials: corsCreds}
		client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		var preHeaders http.Header
		preStatus := 0
		if cors.NeedsPreflight(r) {
			req, err := http.NewRequest("OPTIONS", url, nil)
			if err != nil {
				return err
			}
			req.Header.Set("Origin", r.Origin)
			req.Header.Set("Access-Control-Request-Method", r.Method)
			if len(r.Headers) > 0 {
				req.Header.Set("Access-Control-Request-Headers", strings.ToLower(strings.Join(r.Headers, ", ")))
			}
			resp, err := client.Do(req)
			if err != nil {
				return ui.Errorf("preflight: %v", ui.ShortErr(err))
			}
			_ = resp.Body.Close()
			preHeaders, preStatus = resp.Header, resp.StatusCode
			ui.Println(ui.Label("preflight") + "  OPTIONS " + resp.Status)
			for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Allow-Credentials", "Access-Control-Max-Age"} {
				if v := resp.Header.Get(k); v != "" {
					ui.Println(ui.Dim("  " + k + ": " + v))
				}
			}
		}
		// Сам запрос шлём безопасным методом: проверить заголовки ответа, не меняя данных на сервере.
		method := r.Method
		if method != "GET" && method != "HEAD" {
			method = "HEAD"
		}
		req, err := http.NewRequest(method, url, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Origin", r.Origin)
		resp, err := client.Do(req)
		if err != nil {
			return ui.Errorf("запрос: %v", ui.ShortErr(err))
		}
		_ = resp.Body.Close()
		ui.Println(ui.Label("ответ") + "      " + method + " " + resp.Status)
		for _, k := range []string{"Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers", "Vary"} {
			if v := strings.Join(resp.Header.Values(k), ", "); v != "" {
				ui.Println(ui.Dim("  " + k + ": " + v))
			}
		}
		probs := cors.Analyze(r, preStatus, preHeaders, resp.Header)
		ui.Println()
		if len(probs) == 0 {
			ui.Println(ui.Label("браузер пустит " + r.Method + " с " + r.Origin))
			return nil
		}
		bad := 0
		for _, p := range probs {
			if p.Bad {
				bad++
				ui.Println(ui.Bad(p.Text))
			} else {
				ui.Println(ui.Warn(p.Text))
			}
		}
		if bad > 0 {
			return ui.Errorf("браузер не пустит %s с %s", r.Method, r.Origin)
		}
		return nil
	},
}

func init() {
	corsCmd.Flags().StringVar(&corsOrigin, "origin", "", "откуда запрос, например https://app.example.com")
	corsCmd.Flags().StringVar(&corsMethod, "method", "GET", "метод запроса фронтенда")
	corsCmd.Flags().StringSliceVar(&corsHeaders, "header", nil, "свои заголовки запроса (можно несколько)")
	corsCmd.Flags().BoolVar(&corsCreds, "credentials", false, "запрос с куками (credentials: include)")
	root.AddCommand(corsCmd)
}
