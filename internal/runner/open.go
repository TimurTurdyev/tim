package runner

import (
	"os"
	"runtime"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// OpenBrowser открывает адрес или файл в приложении по умолчанию. Без графики (сервер по ssh)
// открывать нечем - печатаем, что открыть. На Debian open - это openvt, его не трогаем.
func OpenBrowser(target string) error {
	if runtime.GOOS == "darwin" {
		return Run("open", target)
	}
	if (os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "") || !Has("xdg-open") {
		ui.Println(ui.Dim("открыть нечем, адрес: ") + target)
		return nil
	}
	return Run("xdg-open", target)
}

// ToClipboard кладёт текст в буфер обмена: pbcopy на macOS, xclip на Linux; без них - ничего.
func ToClipboard(s string) {
	switch {
	case Has("pbcopy"):
		_, _ = OutputStdin(s, "pbcopy")
	case Has("xclip"):
		_, _ = OutputStdin(s, "xclip", "-selection", "clipboard")
	}
}
