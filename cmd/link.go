package cmd

import (
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/localca"
	"github.com/TimurTurdyev/tim/internal/registry"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/trust"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const linkProfile = "link"

var linkName string

func linkHost(project string) string {
	if linkName != "" {
		return strings.TrimSuffix(linkName, ".")
	}
	return strings.ToLower(project) + ".test"
}

var linkCmd = &cobra.Command{
	Use:   "link [проект] [--name shop.test]",
	Short: "Локальный домен проекту: имя в /etc/hosts, сертификат и url в реестре",
	Long: `Три шага одной командой: имя (по умолчанию <проект>.test) в /etc/hosts в профиле link,
сертификат своим CA в ~/.local/share/tim/certs и url = "https://имя" в реестре.
Дальше tim serve <проект> --https отдаёт проект по этому адресу. Убрать всё обратно: tim unlink.`,
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := registry.Pick(cfg, first(args))
		if err != nil {
			return err
		}
		name := linkHost(p.Name)
		text, entries, err := readHosts()
		if err != nil {
			return err
		}
		ui.Println(ui.Label(p.Name) + "  https://" + name)
		if err := saveHosts(text, hostsfile.Add(entries, "127.0.0.1", []string{name}, linkProfile)); err != nil {
			return err
		}
		ca, fresh, err := localca.LoadOrCreate()
		if err != nil {
			return ui.Errorf("локальный CA: %v", err)
		}
		if fresh {
			ui.Println(ui.Dim("создан локальный CA: " + localca.CertPath()))
		}
		dns, _ := localca.Names([]string{name}, false)
		leaf, err := ca.Issue(dns, nil, localca.LeafDays)
		if err != nil {
			return err
		}
		certPath, keyPath := localca.LeafPaths(name)
		if runner.DryRun {
			ui.Println(ui.Dim("$ # записал бы " + certPath))
		} else if err := leaf.Save(certPath, keyPath); err != nil {
			return err
		}
		p.URL = "https://" + name
		if !runner.DryRun {
			if err := saveConfig(cfg); err != nil {
				return err
			}
		}
		ui.Println(ui.Dim("  сертификат " + certPath))
		if !trust.Installed(localca.Name()) {
			ui.Println(ui.Warn("CA не в хранилище, браузер сертификату не поверит") + ui.Dim("  (tim cert ca install)"))
		}
		ui.Println(ui.Dim("дальше: tim serve " + p.Name + " --https"))
		return nil
	},
}

var unlinkCmd = &cobra.Command{
	Use:               "unlink [проект] [--name shop.test]",
	Short:             "Убрать то, что сделал tim link",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		p, err := registry.Pick(cfg, first(args))
		if err != nil {
			return err
		}
		name := linkHost(p.Name)
		text, entries, err := readHosts()
		if err != nil {
			return err
		}
		if err := saveHosts(text, hostsfile.Remove(entries, []string{name})); err != nil {
			return err
		}
		certPath, keyPath := localca.LeafPaths(name)
		if !runner.DryRun {
			_ = os.Remove(certPath)
			_ = os.Remove(keyPath)
		}
		if p.URL == "https://"+name {
			p.URL = ""
			if !runner.DryRun {
				if err := saveConfig(cfg); err != nil {
					return err
				}
			}
		}
		ui.Println(ui.Label("убрано") + ui.Dim("  "+name))
		return nil
	},
}

func init() {
	for _, c := range []*cobra.Command{linkCmd, unlinkCmd} {
		c.Flags().StringVar(&linkName, "name", "", "имя вместо <проект>.test")
	}
	root.AddCommand(linkCmd, unlinkCmd)
}
