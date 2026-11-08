package local

import (
	"net"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

func addHosts(names []string, ip, profile string) error {
	if net.ParseIP(ip) == nil {
		return ui.Errorf("не адрес: %s", ip)
	}
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	entries = hostsfile.Add(entries, ip, names, profile)
	ui.Println(ui.Label(ip) + "  " + strings.Join(names, " ") + ui.Dim("  профиль "+profileOr(profile)))
	return save(text, entries)
}

func removeHosts(names []string) error {
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	before := len(entries)
	entries = hostsfile.Remove(entries, names)
	if len(entries) == before && nameCount(hostsfile.Parse(text)) == nameCount(entries) {
		return ui.Errorf("таких имён в блоке tim нет: %s", strings.Join(names, " "))
	}
	return save(text, entries)
}

func switchProfile(profile string, on bool) error {
	text, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	entries, n := hostsfile.Switch(entries, profile, on)
	if n == 0 {
		names, _ := hostsfile.Profiles(entries)
		if len(names) == 0 {
			return ui.Errorf("записей tim в %s нет: tim host add <имя>", hostsfile.Path())
		}
		ui.Println(ui.Dim("профиль " + profile + " уже в этом состоянии; есть: " + strings.Join(names, " ")))
		return nil
	}
	word := "включено"
	if !on {
		word = "выключено"
	}
	ui.Println(ui.Label(profile) + "  " + strconv.Itoa(n) + " " + word)
	return save(text, entries)
}

// save переписывает файл hosts; в dry-run показывает, каким стал бы блок tim.
func save(text string, entries []hostsfile.Entry) error {
	if hostsfile.Render(text, entries) == text {
		ui.Println(ui.Dim("в " + hostsfile.Path() + " менять нечего"))
		return nil
	}
	if runner.DryRun {
		runner.Note("записал бы в " + hostsfile.Path() + ":")
		printBlock(entries)
		return nil
	}
	if err := hostsfile.Save(text, entries); err != nil {
		return err
	}
	ui.Println(ui.Dim("прежний файл: " + hostsfile.Path() + ".tim-backup"))
	return nil
}

// nameCount: сколько имён в записях - запись могла остаться, а имя из неё уйти.
func nameCount(entries []hostsfile.Entry) int {
	n := 0
	for _, e := range entries {
		n += len(e.Names)
	}
	return n
}

func hasName(e hostsfile.Entry, name string) bool {
	for _, n := range e.Names {
		if n == name {
			return true
		}
	}
	return false
}

func profileOr(p string) string {
	if p == "" {
		return hostsfile.DefaultProfile
	}
	return p
}

// hostRow: запись tim в hosts для --json.
type hostRow struct {
	IP      string   `json:"ip"`
	Names   []string `json:"names"`
	Profile string   `json:"profile,omitempty"`
	On      bool     `json:"on"`
}

// hostsReport: записи tim в hosts и профили - сколько в каждом включено.
func hostsReport() (report.Report, error) {
	_, entries, err := hostsfile.Load()
	if err != nil {
		return report.Report{}, err
	}
	rows := make([]hostRow, 0, len(entries))
	for _, e := range entries {
		rows = append(rows, hostRow{e.IP, e.Names, e.Profile, e.On})
	}
	r := report.Report{Schema: "tim.host/1", Data: rows, Blocks: []report.Block{blockTable(entries)}}
	if len(entries) == 0 {
		return r, nil
	}
	names, counts := hostsfile.Profiles(entries)
	var profiles report.Facts
	for _, p := range names {
		profiles = append(profiles, report.Fact{Key: p, Value: strconv.Itoa(counts[p][0]) + " из " + strconv.Itoa(counts[p][1]) + " включено"})
	}
	r.Blocks = append(r.Blocks, report.Heading("профили"), profiles, report.Next{{Text: "включить профиль", Command: "tim host on <профиль>"}})
	return r, nil
}

// blockTable: блок tim в hosts; выключенные записи приглушены.
func blockTable(entries []hostsfile.Entry) report.Block {
	if len(entries) == 0 {
		return report.Note("записей tim в " + hostsfile.Path() + " нет")
	}
	t := report.Table{Columns: []report.Column{{Title: "IP", Style: report.Key}, {Title: "ИМЕНА"}, {Title: "ПРОФИЛЬ", Style: report.Muted}, {Title: ""}}}
	for _, e := range entries {
		state := report.Mark{}
		names := any(strings.Join(e.Names, " "))
		if !e.On {
			state = report.Mark{Text: "выключено", Level: report.Info}
			names = report.Mark{Text: strings.Join(e.Names, " "), Level: report.Info}
		}
		t.Rows = append(t.Rows, []any{e.IP, names, e.Profile, state})
	}
	return t
}

func printBlock(entries []hostsfile.Entry) {
	view.Print(report.Report{Blocks: []report.Block{blockTable(entries)}})
}

// hostCheck: куда резолвится имя и есть ли оно в блоке tim - для --json у tim host check.
type hostCheck struct {
	Name    string   `json:"name"`
	IPs     []string `json:"ips,omitempty"`
	Error   string   `json:"error,omitempty"`
	Profile string   `json:"profile,omitempty"`
	On      *bool    `json:"on,omitempty"`
}

func checkHostsReport(names []string) report.Report {
	_, entries, _ := hostsfile.Load()
	rows := make([]hostCheck, 0, len(names))
	var facts report.Facts
	for _, name := range names {
		c := hostCheck{Name: name}
		f := report.Fact{Key: name}
		if ips, err := net.LookupHost(name); err != nil {
			c.Error, f.Value, f.Level = err.Error(), "не резолвится", report.Bad
		} else {
			c.IPs, f.Value = ips, strings.Join(ips, " ")
		}
		for _, e := range entries {
			if !hasName(e, name) {
				continue
			}
			on := e.On
			c.Profile, c.On = e.Profile, &on
			state := "через " + hostsfile.Path() + ", профиль " + e.Profile
			if !e.On {
				state = "в " + hostsfile.Path() + " есть, но выключено, профиль " + e.Profile
			}
			f.Value += "  " + state
		}
		rows = append(rows, c)
		facts = append(facts, f)
	}
	return report.Report{Schema: "tim.host.check/1", Data: rows, Blocks: []report.Block{facts}}
}
