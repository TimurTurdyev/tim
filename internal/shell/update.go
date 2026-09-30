package shell

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"

	"github.com/TimurTurdyev/tim/internal/bypass"
	"github.com/TimurTurdyev/tim/internal/release"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// selfUpdate ставит последний релиз поверх себя; check - только сказать, есть ли он.
func selfUpdate(current string, check bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	rel, err := release.Latest()
	if err != nil {
		return ui.Errorf("последний релиз не получен: %v", err)
	}
	if !release.Newer(rel.Tag, current) {
		ui.Println(ui.Label("уже последняя ") + current)
		return nil
	}
	ui.Println(ui.Info("есть "+rel.Tag) + ui.Dim(", сейчас "+current))
	if check {
		return nil
	}
	if strings.Contains(exe, "/Caskroom/") {
		ui.Println(ui.Dim("tim стоит из Homebrew: brew upgrade --cask tim"))
		return nil
	}
	asset, ok := release.PickAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
	if !ok {
		return ui.Errorf("в релизе %s нет архива для %s/%s", rel.Tag, runtime.GOOS, runtime.GOARCH)
	}
	sum, err := release.Checksum(rel, asset.Name)
	if err != nil {
		return err
	}
	if sum == "" {
		return ui.Errorf("в checksums.txt нет суммы для %s: обновление без проверки не ставлю", asset.Name)
	}
	if runner.DryRun {
		ui.Println(ui.Dim("dry-run: скачал бы " + asset.URL + " и заменил " + exe))
		return nil
	}
	bin, err := release.Binary(asset, sum)
	if err != nil {
		return err
	}
	updated := time.Now()
	if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{TargetPath: exe}); err != nil {
		if rerr := selfupdate.RollbackError(err); rerr != nil {
			return ui.Errorf("замена не удалась и откат тоже: %v; старый файл: %s.old", rerr, exe)
		}
		return ui.Errorf("замена не удалась, оставлена прежняя версия: %v", err)
	}
	ui.Println(ui.Label("обновлено до ") + rel.Tag + ui.Dim("  "+exe))
	bypass.GuardAfterUpdate(updated)
	return nil
}
