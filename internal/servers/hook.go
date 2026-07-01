package servers

import (
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/hook"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// catchHooks: принимать запросы на порт, печатать их разобранными и писать в журнал.
func catchHooks(env *app.Env, args []string, o hookOptions) error {
	j := &hook.Journal{Path: hook.DefaultPath()}
	if o.list {
		for _, r := range j.Load() {
			ui.Println(ui.Label(ui.Pad("#"+strconv.Itoa(r.N), 6)) + r.At.Format("02.01 15:04:05") + "  " + r.Method + " " + r.Path)
		}
		return nil
	}
	port := app.First(args)
	if port == "" {
		port = "9999"
	}
	body := []byte("ok\n")
	if o.body != "" {
		data, err := os.ReadFile(o.body)
		if err != nil {
			return err
		}
		body = data
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		return ui.Errorf("порт %s: %v", port, err)
	}
	srv := &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: hook.Handler(o.status, body, func(r hook.Record) {
		rec, err := j.Add(r)
		if err != nil {
			ui.Println(ui.Warn("журнал: " + err.Error()))
		}
		ui.Println(ui.Label("#"+strconv.Itoa(rec.N)) + " " + rec.At.Format("15:04:05") + "  " + ui.Info(rec.Method+" "+rec.Path) + ui.Dim("  от "+rec.Remote))
		ui.Printf("%s", hook.Pretty(rec))
	})}
	ui.Println(ui.Label("жду запросы") + " http://127.0.0.1:" + port + ui.Dim("  ответ "+strconv.Itoa(o.status)+", журнал "+j.Path))
	if !o.expose {
		return srv.Serve(l)
	}
	go func() { _ = srv.Serve(l) }()
	return expose(env, port, exposeOptions{})
}

func replayHook(arg, to string) error {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return ui.Errorf("номер запроса: %s", arg)
	}
	r, ok := (&hook.Journal{Path: hook.DefaultPath()}).Find(n)
	if !ok {
		return ui.Errorf("запроса #%d в журнале нет: tim hook --list", n)
	}
	if to == "" {
		return ui.Errorf("куда повторить: --to http://localhost:8000")
	}
	resp, err := hook.Replay(r, to, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	ui.Println(ui.Label(r.Method+" "+to+r.Path) + "  " + resp.Status)
	return nil
}
