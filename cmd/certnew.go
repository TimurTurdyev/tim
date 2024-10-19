package cmd

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/trust"
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
		if !trust.Installed(localca.Name()) {
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
		if trust.Installed(localca.Name()) {
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
		if err := trust.Add(localca.CertPath()); err != nil {
			return err
		}
		trust.AddBrowsers(localca.CertPath(), localca.Name())
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
		if err := trust.Remove(localca.CertPath()); err != nil {
			return err
		}
		trust.RemoveBrowsers(localca.Name())
		ui.Println(ui.Label("готово") + ui.Dim("  ранее выпущенные сертификаты перестанут считаться доверенными"))
		return nil
	},
}

func init() {
	certNewCmd.Flags().StringVar(&certDir, "dir", "", "куда положить файлы (по умолчанию текущая папка)")
	certNewCmd.Flags().IntVar(&certDays, "days", localca.LeafDays, "срок в днях")
	certNewCmd.Flags().BoolVar(&certExact, "exact", false, "только перечисленные имена, без *.домен")
	certCaCmd.AddCommand(certCaInstallCmd, certCaUninstallCmd)
	certCmd.AddCommand(certNewCmd, certCaCmd)
}
