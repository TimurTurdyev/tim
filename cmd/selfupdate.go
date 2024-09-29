package cmd

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/release"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const releasesAPI = "https://api.github.com/repos/TimurTurdyev/tim/releases/latest"

var updateClient = &http.Client{Timeout: 60 * time.Second}

func fetch(url string, limit int64) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := updateClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, ui.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

var updateCheck bool

var selfUpdateCmd = &cobra.Command{
	Use:   "self-update [--check]",
	Short: "Обновить tim до последнего релиза на GitHub",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		exe, _ = filepath.EvalSymlinks(exe)
		data, err := fetch(releasesAPI, 1<<20)
		if err != nil {
			return ui.Errorf("последний релиз не получен: %v", err)
		}
		var rel release.Release
		if err := json.Unmarshal(data, &rel); err != nil {
			return err
		}
		current := resolveVersion()
		if !release.Newer(rel.Tag, current) {
			ui.Println(ui.Label("уже последняя ") + current)
			return nil
		}
		ui.Println(ui.Info("есть "+rel.Tag) + ui.Dim(", сейчас "+current))
		if updateCheck {
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
		var sums map[string]string
		for _, a := range rel.Assets {
			if a.Name == "checksums.txt" {
				body, err := fetch(a.URL, 1<<20)
				if err != nil {
					return err
				}
				sums = release.ParseChecksums(bytes.NewReader(body))
			}
		}
		want := sums[asset.Name]
		if want == "" {
			return ui.Errorf("в checksums.txt нет суммы для %s: обновление без проверки не ставлю", asset.Name)
		}
		if runner.DryRun {
			ui.Println(ui.Dim("dry-run: скачал бы " + asset.URL + " и заменил " + exe))
			return nil
		}
		archive, err := fetch(asset.URL, 200<<20)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(archive)
		if hex.EncodeToString(sum[:]) != want {
			return ui.Errorf("сумма %s не совпала с checksums.txt, обновление отменено", asset.Name)
		}
		bin, err := release.ExtractTar(bytes.NewReader(archive), "tim")
		if err != nil {
			return err
		}
		if err := selfupdate.Apply(bytes.NewReader(bin), selfupdate.Options{TargetPath: exe}); err != nil {
			if rerr := selfupdate.RollbackError(err); rerr != nil {
				return ui.Errorf("замена не удалась и откат тоже: %v; старый файл: %s.old", rerr, exe)
			}
			return ui.Errorf("замена не удалась, оставлена прежняя версия: %v", err)
		}
		ui.Println(ui.Label("обновлено до ") + rel.Tag + ui.Dim("  "+exe))
		return nil
	},
}

func init() {
	selfUpdateCmd.Flags().BoolVar(&updateCheck, "check", false, "только проверить, есть ли новая версия")
	root.AddCommand(selfUpdateCmd)
}
