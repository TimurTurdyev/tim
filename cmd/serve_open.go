package cmd

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var serveOpen bool

// openWhenReady открывает адрес, как только порт ответит. Поле url проекта не берём:
// там часто боевой адрес, а открыть надо локальный.
func openWhenReady(port, url string) {
	if runner.DryRun {
		_ = runner.OpenBrowser(url)
		return
	}
	if !netinfo.WaitPort(port, 30*time.Second) {
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
