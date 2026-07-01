package local

import (
	"os"
	"strings"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// linkProfile: профиль в /etc/hosts, куда link кладёт имена проектов.
const linkProfile = "link"

// link: имя в /etc/hosts, сертификат своим CA и url = "https://имя" в реестре.
func link(env *app.Env, project, name string) error {
	p, err := env.Project(project)
	if err != nil {
		return err
	}
	host := linkHost(p.Name, name)
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	ui.Println(ui.Label(p.Name) + "  https://" + host)
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
	p.URL = "https://" + host
	if err := env.Save(); err != nil {
		return err
	}
	ui.Println(ui.Dim("  сертификат " + certPath))
	warnUntrusted()
	ui.Println(ui.Dim("дальше: tim serve " + p.Name + " --https"))
	return nil
}

// unlink убирает то, что сделал link: имя, сертификат и url.
func unlink(env *app.Env, project, name string) error {
	p, err := env.Project(project)
	if err != nil {
		return err
	}
	host := linkHost(p.Name, name)
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
	if p.URL == "https://"+host {
		p.URL = ""
		if err := env.Save(); err != nil {
			return err
		}
	}
	ui.Println(ui.Label("убрано") + ui.Dim("  "+host))
	return nil
}

func linkHost(project, name string) string {
	if name != "" {
		return strings.TrimSuffix(name, ".")
	}
	return hostsfile.LocalName(project)
}
