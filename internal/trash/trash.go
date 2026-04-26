// Package trash: в Корзину, откуда можно вернуть, а не rm.
package trash

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Move: в Корзину, откуда можно вернуть, а не rm.
func Move(path string) error {
	switch {
	case runner.Has("trash") && runtime.GOOS == "darwin":
		return runner.Run("trash", path)
	case runner.Has("gio"):
		return runner.Run("gio", "trash", path)
	case runner.Has("trash-put"):
		return runner.Run("trash-put", path)
	case runtime.GOOS == "linux":
		return freedesktop(path)
	}
	return ui.Errorf("не нашёл, чем класть в Корзину (trash на macOS, gio на Linux); %s не тронут", path)
}

// freedesktop: Корзина по спецификации freedesktop - так её видят файловые менеджеры Linux
// и trash-cli. Работает в пределах одного диска: перенос, а не копирование.
func freedesktop(path string) error {
	dir := filepath.Join(paths.Home(), ".local", "share", "Trash")
	name := filepath.Base(path) + "." + strconv.FormatInt(time.Now().Unix(), 10)
	info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", path, time.Now().Format("2006-01-02T15:04:05"))
	ui.Println(ui.Dim("$ mv " + shellq.Join([]string{path, filepath.Join(dir, "files", name)})))
	if runner.DryRun {
		return nil
	}
	for _, sub := range []string{"files", "info"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
			return err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "info", name+".trashinfo"), []byte(info), 0o600); err != nil {
		return err
	}
	if err := os.Rename(path, filepath.Join(dir, "files", name)); err != nil {
		return ui.Errorf("в Корзину не перенести (другой диск?): %v; %s не тронут", err, path)
	}
	return nil
}
