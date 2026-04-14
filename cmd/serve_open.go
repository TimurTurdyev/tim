package cmd

import (
	"net"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var serveOpen bool

// waitPort ждёт, пока на порту кто-то начнёт принимать соединения.
func waitPort(port string, limit time.Duration) bool {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+port, 300*time.Millisecond); err == nil {
			_ = c.Close()
			return true
		}
		time.Sleep(250 * time.Millisecond)
	}
	return false
}

// openWhenReady открывает адрес, как только порт ответит. Поле url проекта не берём:
// там часто боевой адрес, а открыть надо локальный.
func openWhenReady(port, url string) {
	if runner.DryRun {
		_ = runner.OpenBrowser(url)
		return
	}
	if !waitPort(port, 30*time.Second) {
		ui.Println(ui.Warn("порт " + port + " за 30 с не ответил, браузер не открываю"))
		return
	}
	if err := runner.OpenBrowser(url); err != nil {
		ui.Println(ui.Warn("не открыл " + url + ": " + err.Error()))
	}
}

func init() {
	serveCmd.Flags().BoolVar(&serveOpen, "open", false, "открыть в браузере, когда порт ответит")
}
