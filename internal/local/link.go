package local

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// linkProfile: профиль в /etc/hosts, куда link кладёт локальные имена папок.
const linkProfile = "link"

// link: имя в /etc/hosts и сертификат своим CA.
func link(base, name string) error {
	host := linkHost(base, name)
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	ui.Println(ui.Label("https://" + host))
	if err := save(text, hostsfile.Add(entries, "127.0.0.1", []string{host}, linkProfile)); err != nil {
		return err
	}
	ca, fresh, err := localca.LoadOrCreate()
	if err != nil {
		return ui.Errorf("локальный CA: %v", err)
	}
	if fresh && !runner.DryRun {
		ui.Println(ui.Dim("создан локальный CA: " + localca.CertPath()))
	}
	dns, _ := localca.Names([]string{host}, false)
	leaf, err := ca.Issue(dns, nil, localca.LeafDays)
	if err != nil {
		return err
	}
	certPath, keyPath := localca.LeafPaths(host)
	if err := leaf.Save(certPath, keyPath); err != nil {
		return err
	}
	ui.Println(ui.Dim("  сертификат " + certPath))
	ui.Println(ui.Dim("  ключ       " + keyPath))
	warnUntrusted()
	ui.Println(ui.Dim("дальше: подключи сертификат и ключ к dev-серверу, например vite --https с server.https в конфиге"))
	return nil
}

// unlink убирает то, что сделал link: имя и сертификат.
func unlink(base, name string) error {
	host := linkHost(base, name)
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	if err := save(text, hostsfile.Remove(entries, []string{host})); err != nil {
		return err
	}
	certPath, keyPath := localca.LeafPaths(host)
	if !runner.DryRun {
		_ = os.Remove(certPath)
		_ = os.Remove(keyPath)
	}
	ui.Println(ui.Label("убрано") + ui.Dim("  "+host))
	return nil
}

// linkHost: имя целиком из --name, иначе <имя>.test, а без имени - по текущей папке.
func linkHost(base, name string) string {
	if name != "" {
		return strings.TrimSuffix(name, ".")
	}
	if base == "" {
		cwd, _ := os.Getwd()
		base = filepath.Base(cwd)
	}
	return hostsfile.LocalName(base)
}
