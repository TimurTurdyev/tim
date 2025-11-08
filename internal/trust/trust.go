// Package trust: установка своего корневого сертификата в хранилища доверия системы и браузеров.
// Что именно устанавливается, пакет не решает: путь и имя приходят снаружи.
package trust

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// anchors: куда в этой системе кладут свои корневые сертификаты.
func anchors() []string {
	var out []string
	for _, a := range []struct{ dir, name string }{
		{"/usr/local/share/ca-certificates", "tim-local-ca.crt"},
		{"/etc/pki/ca-trust/source/anchors", "tim-local-ca.pem"},
		{"/etc/ca-certificates/trust-source/anchors", "tim-local-ca.crt"},
	} {
		if st, err := os.Stat(a.dir); err == nil && st.IsDir() {
			out = append(out, filepath.Join(a.dir, a.name))
		}
	}
	return out
}

// Installed: виден ли сертификат с таким именем в системном хранилище.
func Installed(name string) bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := runner.Output("security", "find-certificate", "-a", "-c", name,
			"/Library/Keychains/System.keychain")
		return err == nil && strings.Contains(out, "keychain")
	case "linux":
		for _, p := range anchors() {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	return false
}

// Add добавляет сертификат в системное хранилище.
func Add(certPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return runner.Sudo("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", certPath)
	case "linux":
		list := anchors()
		if len(list) == 0 {
			return ui.Errorf("не нашёл, куда класть свои корневые сертификаты в этой системе")
		}
		data, err := os.ReadFile(certPath)
		if err != nil {
			return err
		}
		if err := runner.WriteRoot(list[0], data); err != nil {
			return err
		}
		return refresh(list[0])
	}
	return ui.Errorf("установка в хранилище на %s пока не поддержана: добавь %s вручную", runtime.GOOS, certPath)
}

// Remove убирает сертификат из системного хранилища.
func Remove(certPath string) error {
	switch runtime.GOOS {
	case "darwin":
		return runner.Sudo("security", "remove-trusted-cert", "-d", certPath)
	case "linux":
		for _, p := range anchors() {
			if _, err := os.Stat(p); err == nil {
				if err := runner.RemoveRoot(p); err != nil {
					return err
				}
			}
		}
		return refresh("")
	}
	return ui.Errorf("удаление из хранилища на %s пока не поддержано", runtime.GOOS)
}

func refresh(placed string) error {
	for _, up := range []string{"update-ca-certificates", "update-ca-trust"} {
		if runner.Has(up) {
			return runner.Sudo(up)
		}
	}
	if runner.Has("trust") {
		return runner.Sudo("trust", "extract-compat")
	}
	if placed == "" {
		return nil
	}
	return ui.Errorf("сертификат положен в %s, но обновлялку хранилища я не нашёл", placed)
}

// Profiles: профили Firefox - у него своё хранилище, системному он не верит.
func Profiles() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var roots []string
	switch runtime.GOOS {
	case "darwin":
		roots = []string{filepath.Join(home, "Library", "Application Support", "Firefox", "Profiles", "*")}
	case "linux":
		roots = []string{
			filepath.Join(home, ".mozilla", "firefox", "*"),
			filepath.Join(home, "snap", "firefox", "common", ".mozilla", "firefox", "*"),
		}
	}
	var out []string
	for _, pattern := range roots {
		dirs, _ := filepath.Glob(pattern)
		for _, d := range dirs {
			if _, err := os.Stat(filepath.Join(d, "cert9.db")); err == nil {
				out = append(out, d)
			}
		}
	}
	return out
}

// AddBrowsers добавляет сертификат в профили Firefox; без certutil это невозможно.
func AddBrowsers(certPath, name string) {
	profiles := Profiles()
	if len(profiles) == 0 {
		return
	}
	if !runner.Has("certutil") {
		ui.Println(ui.Dim("Firefox держит своё хранилище: поставь certutil (" + runner.InstallHint("nss", "libnss3-tools") + ") и повтори"))
		return
	}
	for _, d := range profiles {
		if err := runner.Run("certutil", "-A", "-d", "sql:"+d, "-t", "C,,", "-n", name, "-i", certPath); err != nil {
			ui.Println(ui.Warn("Firefox " + filepath.Base(d) + ": " + err.Error()))
		}
	}
}

// RemoveBrowsers убирает сертификат из профилей Firefox.
func RemoveBrowsers(name string) {
	if !runner.Has("certutil") {
		return
	}
	for _, d := range Profiles() {
		_ = runner.Run("certutil", "-D", "-d", "sql:"+d, "-n", name)
	}
}
