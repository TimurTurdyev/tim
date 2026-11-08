package shell

import (
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

// configData: сводка конфига для --json - без самих значений вроде токенов уведомлений.
type configData struct {
	Path    string   `json:"path"`
	Dev     string   `json:"dev"`
	Targets int      `json:"bypass_targets"`
	Sites   []string `json:"sites,omitempty"`
	Motd    bool     `json:"motd"`
	Aliases int      `json:"aliases"`
	Legacy  string   `json:"legacy,omitempty"`
}

// printConfig: где конфиг и что в нём - сводкой; весь файл - с raw.
func printConfig(cfg *config.Config, raw bool) error {
	if raw && !view.AsJSON {
		ui.Println(ui.Dim(config.Path()))
		return toml.NewEncoder(ui.Writer()).Encode(cfg)
	}
	d := configData{Path: config.Path(), Dev: cfg.Dev, Targets: len(cfg.Hosts), Sites: cfg.SiteList(), Motd: cfg.Shell.Motd,
		Aliases: len(cfg.Shell.Aliases), Legacy: cfg.Legacy()}
	facts := report.Facts{{Key: "конфиг", Value: d.Path}, {Key: "код", Value: d.Dev + " - здесь Диск ищет node_modules, vendor и сборки"}}
	if d.Targets > 0 {
		facts = append(facts, report.Fact{Key: "обход", Value: ui.Count(d.Targets, "цель", "цели", "целей") + " - tim route"})
	}
	if len(d.Sites) > 0 {
		facts = append(facts, report.Fact{Key: "сайты", Value: strings.Join(d.Sites, ", ")})
	}
	sh := "сводка при входе " + onOff(d.Motd)
	if d.Aliases > 0 {
		sh += ", алиасов " + strconv.Itoa(d.Aliases)
	}
	facts = append(facts, report.Fact{Key: "оболочка", Value: sh})
	if d.Legacy != "" {
		facts = append(facts, report.Fact{Key: "лишнее", Value: d.Legacy + " - tim их больше не читает, можно убрать руками", Level: report.Info})
	}
	return view.Show(report.Report{Schema: "tim.config/1", Data: d, Blocks: []report.Block{facts,
		report.Note("весь файл: tim config --raw; править можно руками, tim перечитывает сам")}}, view.AsJSON)
}

func onOff(on bool) string {
	if on {
		return "да"
	}
	return "нет"
}
