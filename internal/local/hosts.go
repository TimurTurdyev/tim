package local

import (
	"net"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

func listHosts() error {
	_, entries, err := hostsfile.Load()
	if err != nil {
		return err
	}
	printBlock(entries)
	names, counts := hostsfile.Profiles(entries)
	for _, p := range names {
		c := counts[p]
		ui.Println(ui.Label(ui.Pad(p, 16)) + ui.Dim(strconv.Itoa(c[0])+" из "+strconv.Itoa(c[1])+" включено"))
	}
	if len(entries) > 0 {
		ui.Println(ui.Dim("включить профиль: tim host on <профиль>"))
	}
	return nil
}

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

// checkHosts: куда резолвится имя и есть ли оно в блоке tim.
func checkHosts(names []string) {
	_, entries, _ := hostsfile.Load()
	for _, name := range names {
		line := ui.Pad(name, 30)
		if ips, err := net.LookupHost(name); err != nil {
			line += ui.Bad("не резолвится")
		} else {
			line += strings.Join(ips, " ")
		}
		for _, e := range entries {
			if !hasName(e, name) {
				continue
			}
			state := "через " + hostsfile.Path() + ", профиль " + e.Profile
			if !e.On {
				state = "в " + hostsfile.Path() + " есть, но выключено, профиль " + e.Profile
			}
			line += ui.Dim("  " + state)
		}
		ui.Println(line)
	}
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

func printBlock(entries []hostsfile.Entry) {
	if len(entries) == 0 {
		ui.Println(ui.Dim("записей tim в " + hostsfile.Path() + " нет"))
		return
	}
	for _, e := range entries {
		line := ui.Pad(e.IP, 16) + ui.Pad(strings.Join(e.Names, " "), 40) + ui.Dim(e.Profile)
		if !e.On {
			line = ui.Dim(ui.Pad(e.IP, 16)+ui.Pad(strings.Join(e.Names, " "), 40)+e.Profile) + " " + ui.Dim("выключено")
		}
		ui.Println(line)
	}
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
