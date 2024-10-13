package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	certDir   string
	certDays  int
	certExact bool
)

var certNewCmd = &cobra.Command{
	Use:   "new <домен|адрес...> [--dir] [--days] [--exact]",
	Short: "Выпустить сертификат для локальной разработки своим CA",
	Long: `Подписывает сертификат локальным CA из ~/.local/share/tim/ca; при первом запуске CA создаётся.
К каждому домену добавляется и "*.домен", потому что wildcard не покрывает сам домен (--exact отключает).
Чтобы браузер доверял сертификату, CA должен быть в хранилище: tim cert ca install.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dns, ips := localca.Names(args, certExact)
		if len(dns) == 0 && len(ips) == 0 {
			return ui.Errorf("не понял ни одного имени")
		}
		ca, fresh, err := localca.LoadOrCreate()
		if err != nil {
			return ui.Errorf("локальный CA: %v", err)
		}
		if fresh {
			ui.Println(ui.Label("создан CA") + "  " + ui.Dim(localca.CertPath()))
			ui.Println(ui.Dim("чтобы браузеры ему доверяли: tim cert ca install"))
		}
		leaf, err := ca.Issue(dns, ips, certDays)
		if err != nil {
			return err
		}
		dir := certDir
		if dir == "" {
			if dir, err = os.Getwd(); err != nil {
				return err
			}
		}
		stem := localca.FileStem(dns, ips)
		certPath := filepath.Join(dir, stem+".pem")
		keyPath := filepath.Join(dir, stem+"-key.pem")
		if runner.DryRun {
			ui.Println(ui.Dim("$ # записал бы " + certPath + " и " + keyPath))
			return nil
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(certPath, leaf.Cert, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(keyPath, leaf.Key, 0o600); err != nil {
			return err
		}
		names := append(append([]string{}, dns...), ipStrings(leaf)...)
		ui.Println(ui.Label("сертификат") + "  " + strings.Join(names, " "))
		ui.Println(ui.Pad("  файлы", 12) + certPath + " " + ui.Dim("и ключ рядом"))
		ui.Println(ui.Pad("  годен", 12) + daysLeft(leaf.Expires))
		if !caInstalled() {
			ui.Println(ui.Warn("CA не в хранилище, браузер сертификату не поверит") + ui.Dim("  (tim cert ca install)"))
		}
		return nil
	},
}

func ipStrings(l *localca.Leaf) []string {
	out := make([]string, 0, len(l.IPs))
	for _, ip := range l.IPs {
		out = append(out, ip.String())
	}
	return out
}

// caInstalled: виден ли наш CA в системном хранилище.
func caInstalled() bool {
	switch runtime.GOOS {
	case "darwin":
		out, err := runner.Output("security", "find-certificate", "-a", "-c", localca.Name(),
			"/Library/Keychains/System.keychain")
		return err == nil && strings.Contains(out, "keychain")
	case "linux":
		for _, p := range linuxAnchors() {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
	}
	return false
}

// linuxAnchors: куда в этом дистрибутиве кладут свои корневые сертификаты.
func linuxAnchors() []string {
	var out []string
	for dir, name := range map[string]string{
		"/usr/local/share/ca-certificates":          "tim-local-ca.crt",
		"/etc/pki/ca-trust/source/anchors":          "tim-local-ca.pem",
		"/etc/ca-certificates/trust-source/anchors": "tim-local-ca.crt",
	} {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			out = append(out, filepath.Join(dir, name))
		}
	}
	return out
}

var certCaCmd = &cobra.Command{
	Use:   "ca",
	Short: "Локальный CA: где лежит, до каких пор годен, в хранилище ли он",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ca, err := localca.Load()
		if err != nil {
			ui.Println(ui.Dim("CA ещё нет, создастся при первом tim cert new"))
			ui.Println(ui.Dim("будет лежать в " + localca.Root()))
			return nil
		}
		ui.Println(ui.Label("имя   ") + ca.Cert.Subject.CommonName)
		ui.Println(ui.Label("файл  ") + localca.CertPath())
		ui.Println(ui.Label("годен ") + daysLeft(ca.Cert.NotAfter))
		if caInstalled() {
			ui.Println(ui.Label("в хранилище ") + "да")
		} else {
			ui.Println(ui.Warn("в хранилище не установлен") + ui.Dim("  (tim cert ca install)"))
		}
		return nil
	},
}

var certCaInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Добавить локальный CA в системное хранилище доверия (sudo)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ca, fresh, err := localca.LoadOrCreate()
		if err != nil {
			return err
		}
		if fresh {
			ui.Println(ui.Label("создан CA") + "  " + ui.Dim(localca.CertPath()))
		}
		ui.Println(ui.Dim("после установки система будет доверять любому сертификату, выпущенному этим CA"))
		ui.Println(ui.Dim("ключ CA лежит в " + localca.KeyPath() + ", держи его при себе"))
		if !runner.Confirm("добавить " + ca.Cert.Subject.CommonName + " в хранилище?") {
			return ui.Errorf("отменено")
		}
		if err := trustAdd(); err != nil {
			return err
		}
		nssAdd()
		ui.Println(ui.Label("готово") + ui.Dim("  перезапусти браузер, чтобы он перечитал хранилище"))
		return nil
	},
}

var certCaUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Убрать локальный CA из системного хранилища (sudo)",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if _, err := localca.Load(); err != nil {
			return ui.Errorf("локального CA нет: %v", err)
		}
		if !runner.Confirm("убрать локальный CA из хранилища?") {
			return ui.Errorf("отменено")
		}
		if err := trustRemove(); err != nil {
			return err
		}
		nssRemove()
		ui.Println(ui.Label("готово") + ui.Dim("  ранее выпущенные сертификаты перестанут считаться доверенными"))
		return nil
	},
}

func trustAdd() error {
	switch runtime.GOOS {
	case "darwin":
		return runner.Sudo("security", "add-trusted-cert", "-d", "-r", "trustRoot",
			"-k", "/Library/Keychains/System.keychain", localca.CertPath())
	case "linux":
		anchors := linuxAnchors()
		if len(anchors) == 0 {
			return ui.Errorf("не нашёл, куда класть свои корневые сертификаты в этой системе")
		}
		if err := runner.Sudo("cp", localca.CertPath(), anchors[0]); err != nil {
			return err
		}
		for _, up := range []string{"update-ca-certificates", "update-ca-trust", "trust"} {
			if runner.Has(up) {
				if up == "trust" {
					return runner.Sudo("trust", "extract-compat")
				}
				return runner.Sudo(up)
			}
		}
		return ui.Errorf("сертификат положен в %s, но обновлялку хранилища я не нашёл", anchors[0])
	}
	return ui.Errorf("установка CA на %s пока не поддержана: добавь %s вручную", runtime.GOOS, localca.CertPath())
}

func trustRemove() error {
	switch runtime.GOOS {
	case "darwin":
		return runner.Sudo("security", "remove-trusted-cert", "-d", localca.CertPath())
	case "linux":
		for _, p := range linuxAnchors() {
			if _, err := os.Stat(p); err == nil {
				if err := runner.Sudo("rm", "-f", p); err != nil {
					return err
				}
			}
		}
		for _, up := range []string{"update-ca-certificates", "update-ca-trust"} {
			if runner.Has(up) {
				return runner.Sudo(up)
			}
		}
		return nil
	}
	return ui.Errorf("удаление CA на %s пока не поддержано", runtime.GOOS)
}

// nssProfiles: профили Firefox и Chromium: у них своё хранилище, системному они не верят.
func nssProfiles() []string {
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

func nssAdd() {
	if !runner.Has("certutil") {
		if len(nssProfiles()) > 0 {
			ui.Println(ui.Dim("Firefox держит своё хранилище: поставь certutil (brew install nss) и повтори"))
		}
		return
	}
	for _, d := range nssProfiles() {
		if err := runner.Run("certutil", "-A", "-d", "sql:"+d, "-t", "C,,",
			"-n", localca.Name(), "-i", localca.CertPath()); err != nil {
			ui.Println(ui.Warn("Firefox " + filepath.Base(d) + ": " + err.Error()))
		}
	}
}

func nssRemove() {
	if !runner.Has("certutil") {
		return
	}
	for _, d := range nssProfiles() {
		_ = runner.Run("certutil", "-D", "-d", "sql:"+d, "-n", localca.Name())
	}
}

func init() {
	certNewCmd.Flags().StringVar(&certDir, "dir", "", "куда положить файлы (по умолчанию текущая папка)")
	certNewCmd.Flags().IntVar(&certDays, "days", localca.LeafDays, "срок в днях")
	certNewCmd.Flags().BoolVar(&certExact, "exact", false, "только перечисленные имена, без *.домен")
	certCaCmd.AddCommand(certCaInstallCmd, certCaUninstallCmd)
	certCmd.AddCommand(certNewCmd, certCaCmd)
}
