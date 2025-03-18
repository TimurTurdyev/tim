package cmd

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/hook"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	hookStatus int
	hookBody   string
	hookExpose bool
	hookList   bool
)

func printRecord(r hook.Record) {
	ui.Println(ui.Label("#"+strconv.Itoa(r.N)) + " " + r.At.Format("15:04:05") + "  " + ui.Info(r.Method+" "+r.Path) + ui.Dim("  от "+r.Remote))
	ui.Printf("%s", hook.Pretty(r))
}

var hookCmd = &cobra.Command{
	Use:   "hook [порт] [--status 200] [--body файл] [--expose]",
	Short: "Приёмник вебхуков: каждый запрос разобранным, журнал и повтор в свой сервер",
	Long: `Принимает любые запросы на порт (по умолчанию 9999) и печатает их разобранными: заголовки,
JSON с отступами, поля формы. Всё пишется в журнал - tim hook replay <n> --to <адрес> повторит
запрос в dev-сервер. --expose даёт публичный адрес через cloudflared (как tim tunnel expose).
Данные платёжки не уходят третьему сервису, как с webhook.site.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if hookList {
			for _, r := range (&hook.Journal{Path: hook.DefaultPath()}).Load() {
				ui.Println(ui.Label(ui.Pad("#"+strconv.Itoa(r.N), 6)) + r.At.Format("02.01 15:04:05") + "  " + r.Method + " " + r.Path)
			}
			return nil
		}
		port := "9999"
		if len(args) == 1 {
			port = args[0]
		}
		body := []byte("ok\n")
		if hookBody != "" {
			data, err := os.ReadFile(hookBody)
			if err != nil {
				return err
			}
			body = data
		}
		j := &hook.Journal{Path: hook.DefaultPath()}
		l, err := net.Listen("tcp", "127.0.0.1:"+port)
		if err != nil {
			return ui.Errorf("порт %s: %v", port, err)
		}
		srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: hook.Handler(hookStatus, body, func(r hook.Record) {
			rec, err := j.Add(r)
			if err != nil {
				ui.Println(ui.Warn("журнал: " + err.Error()))
			}
			printRecord(rec)
		})}
		ui.Println(ui.Label("жду запросы") + " http://127.0.0.1:" + port + ui.Dim("  ответ "+strconv.Itoa(hookStatus)+", журнал "+j.Path))
		if !hookExpose {
			return srv.Serve(l)
		}
		go func() { _ = srv.Serve(l) }()
		return tunnelExposeCmd.RunE(tunnelExposeCmd, []string{port})
	},
}

var hookReplayTo string

var hookReplayCmd = &cobra.Command{
	Use:   "replay <n> --to http://localhost:8000",
	Short: "Повторить запрос из журнала в свой сервер",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		n, err := strconv.Atoi(args[0])
		if err != nil {
			return ui.Errorf("номер запроса: %s", args[0])
		}
		r, ok := (&hook.Journal{Path: hook.DefaultPath()}).Find(n)
		if !ok {
			return ui.Errorf("запроса #%d в журнале нет: tim hook --list", n)
		}
		if hookReplayTo == "" {
			return ui.Errorf("куда повторить: --to http://localhost:8000")
		}
		resp, err := hook.Replay(r, hookReplayTo, &http.Client{Timeout: 30 * time.Second})
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		ui.Println(ui.Label(r.Method+" "+hookReplayTo+r.Path) + "  " + resp.Status)
		return nil
	},
}

func init() {
	hookCmd.Flags().IntVar(&hookStatus, "status", 200, "каким кодом отвечать")
	hookCmd.Flags().StringVar(&hookBody, "body", "", "файл с телом ответа")
	hookCmd.Flags().BoolVar(&hookExpose, "expose", false, "публичный адрес через cloudflared")
	hookCmd.Flags().BoolVar(&hookList, "list", false, "что лежит в журнале")
	hookReplayCmd.Flags().StringVar(&hookReplayTo, "to", "", "адрес своего сервера")
	hookCmd.AddCommand(hookReplayCmd)
	root.AddCommand(hookCmd)
}
