package local

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/trust"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type certOptions struct {
	dir   string
	days  int
	exact bool
}

// issueCert выпускает сертификат локальным CA и кладёт его в папку.
func issueCert(names []string, o certOptions) error {
	dns, ips := localca.Names(names, o.exact)
	if len(dns) == 0 && len(ips) == 0 {
		return ui.Errorf("не понял ни одного имени")
	}
	ca, fresh, err := localca.LoadOrCreate()
	if err != nil {
		return ui.Errorf("локальный CA: %v", err)
	}
	if fresh && !runner.DryRun {
		ui.Println(ui.Label("создан CA") + "  " + ui.Dim(localca.CertPath()))
		ui.Println(ui.Dim("чтобы браузеры ему доверяли: tim cert ca install"))
	}
	leaf, err := ca.Issue(dns, ips, o.days)
	if err != nil {
		return err
	}
	dir := o.dir
	if dir == "" {
		if dir, err = os.Getwd(); err != nil {
			return err
		}
	}
	stem := localca.FileStem(dns, ips)
	certPath := filepath.Join(dir, stem+".pem")
	if err := leaf.Save(certPath, filepath.Join(dir, stem+"-key.pem")); err != nil {
		return err
	}
	if runner.DryRun {
		return nil
	}
	all := append([]string{}, dns...)
	for _, ip := range leaf.IPs {
		all = append(all, ip.String())
	}
	ui.Println(ui.Label("сертификат") + "  " + strings.Join(all, " "))
	ui.Println(ui.Pad("  файлы", 12) + certPath + " " + ui.Dim("и ключ рядом"))
	ui.Println(ui.Pad("  годен", 12) + ui.DaysLeft(leaf.Expires))
	warnUntrusted()
	return nil
}

func showCA() {
	ca, err := localca.Load()
	if err != nil {
		ui.Println(ui.Dim("CA ещё нет, создастся при первом tim cert new"))
		ui.Println(ui.Dim("будет лежать в " + localca.Root()))
		return
	}
	ui.Println(ui.Label("имя   ") + ca.Cert.Subject.CommonName)
	ui.Println(ui.Label("файл  ") + localca.CertPath())
	ui.Println(ui.Label("годен ") + ui.DaysLeft(ca.Cert.NotAfter))
	if trust.Installed(localca.Name()) {
		ui.Println(ui.Label("в хранилище ") + "да")
	} else {
		ui.Println(ui.Warn("в хранилище не установлен") + ui.Dim("  (tim cert ca install)"))
	}
}

func trustCA() error {
	ca, fresh, err := localca.LoadOrCreate()
	if err != nil {
		return err
	}
	if fresh && !runner.DryRun {
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
}

func distrustCA() error {
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
}

func warnUntrusted() {
	if !trust.Installed(localca.Name()) {
		ui.Println(ui.Warn("CA не в хранилище, браузер сертификату не поверит") + ui.Dim("  (tim cert ca install)"))
	}
}
