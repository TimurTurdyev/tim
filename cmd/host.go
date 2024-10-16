package cmd

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/hostsfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

var (
	hostIP      string
	hostProfile string
)

// hostsFile: обычно /etc/hosts, но TIM_HOSTS позволяет работать с другим файлом -
// в контейнере, в тесте или когда правами на системный файл не распоряжаешься.
func hostsFile() string {
	if p := os.Getenv("TIM_HOSTS"); p != "" {
		return p
	}
	return "/etc/hosts"
}

func readHosts() (string, []hostsfile.Entry, error) {
	data, err := os.ReadFile(hostsFile())
	if err != nil {
		return "", nil, err
	}
	return string(data), hostsfile.Parse(string(data)), nil
}

// saveHosts переписывает /etc/hosts через sudo, оставляя рядом копию прежнего файла.
func saveHosts(text string, entries []hostsfile.Entry) error {
	next := hostsfile.Render(text, entries)
	if next == text {
		ui.Println(ui.Dim("в " + hostsFile() + " менять нечего"))
		return nil
	}
	if runner.DryRun {
		ui.Println(ui.Dim("$ # записал бы в " + hostsFile() + ":"))
		printHostBlock(entries)
		return nil
	}
	if err := os.WriteFile(hostsFile()+".tim-backup", []byte(text), 0o644); err == nil {
		// Файл наш, sudo не нужен: так бывает в контейнере и при TIM_HOSTS.
		if err := os.WriteFile(hostsFile(), []byte(next), 0o644); err != nil {
			return err
		}
		ui.Println(ui.Dim("прежний файл: " + hostsFile() + ".tim-backup"))
		return nil
	}
	tmp := filepath.Join(os.TempDir(), "tim-hosts")
	if err := os.WriteFile(tmp, []byte(next), 0o644); err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp) }()
	if err := runner.Sudo("cp", hostsFile(), hostsFile()+".tim-backup"); err != nil {
		return err
	}
	if err := runner.Sudo("cp", tmp, hostsFile()); err != nil {
		return err
	}
	ui.Println(ui.Dim("прежний файл: " + hostsFile() + ".tim-backup"))
	return nil
}

func printHostBlock(entries []hostsfile.Entry) {
	if len(entries) == 0 {
		ui.Println(ui.Dim("записей tim в " + hostsFile() + " нет"))
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

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Локальные имена в /etc/hosts: профили, включение и выключение",
	Long: `tim ведёт в /etc/hosts свой блок между "# tim:begin" и "# tim:end" и не трогает ничего за его
пределами. Записи разложены по профилям: профиль целиком включается и выключается одной командой.
Перед каждой записью файл копируется в /etc/hosts.tim-backup.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		_, entries, err := readHosts()
		if err != nil {
			return err
		}
		printHostBlock(entries)
		names, counts := hostsfile.Profiles(entries)
		for _, p := range names {
			c := counts[p]
			ui.Println(ui.Label(ui.Pad(p, 16)) + ui.Dim(strconv.Itoa(c[0])+" из "+strconv.Itoa(c[1])+" включено"))
		}
		if len(entries) > 0 {
			ui.Println(ui.Dim("включить профиль: tim host on <профиль>"))
		}
		return nil
	},
}

var hostAddCmd = &cobra.Command{
	Use:   "add <имя...> [--ip 127.0.0.1] [--profile имя]",
	Short: "Добавить имена в /etc/hosts (sudo)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if net.ParseIP(hostIP) == nil {
			return ui.Errorf("не адрес: %s", hostIP)
		}
		text, entries, err := readHosts()
		if err != nil {
			return err
		}
		entries = hostsfile.Add(entries, hostIP, args, hostProfile)
		ui.Println(ui.Label(hostIP) + "  " + strings.Join(args, " ") + ui.Dim("  профиль "+profileOr(hostProfile)))
		return saveHosts(text, entries)
	},
}

var hostRmCmd = &cobra.Command{
	Use:   "rm <имя...>",
	Short: "Убрать имена из /etc/hosts (sudo)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text, entries, err := readHosts()
		if err != nil {
			return err
		}
		before := len(entries)
		entries = hostsfile.Remove(entries, args)
		if len(entries) == before && !changedNames(hostsfile.Parse(text), entries) {
			return ui.Errorf("таких имён в блоке tim нет: %s", strings.Join(args, " "))
		}
		return saveHosts(text, entries)
	},
}

// changedNames: изменился ли состав имён, даже если число записей осталось прежним.
func changedNames(before, after []hostsfile.Entry) bool {
	count := func(e []hostsfile.Entry) int {
		n := 0
		for _, x := range e {
			n += len(x.Names)
		}
		return n
	}
	return count(before) != count(after)
}

func profileOr(p string) string {
	if p == "" {
		return hostsfile.DefaultProfile
	}
	return p
}

func hostSwitch(profile string, on bool) error {
	text, entries, err := readHosts()
	if err != nil {
		return err
	}
	entries, n := hostsfile.Switch(entries, profile, on)
	if n == 0 {
		names, _ := hostsfile.Profiles(entries)
		if len(names) == 0 {
			return ui.Errorf("записей tim в %s нет: tim host add <имя>", hostsFile())
		}
		ui.Println(ui.Dim("профиль " + profile + " уже в этом состоянии; есть: " + strings.Join(names, " ")))
		return nil
	}
	word := "включено"
	if !on {
		word = "выключено"
	}
	ui.Println(ui.Label(profile) + "  " + strconv.Itoa(n) + " " + word)
	return saveHosts(text, entries)
}

var hostOnCmd = &cobra.Command{
	Use:   "on <профиль>",
	Short: "Включить записи профиля (sudo)",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return hostSwitch(args[0], true) },
}

var hostOffCmd = &cobra.Command{
	Use:   "off <профиль>",
	Short: "Выключить записи профиля, не удаляя их (sudo)",
	Args:  cobra.ExactArgs(1),
	RunE:  func(cmd *cobra.Command, args []string) error { return hostSwitch(args[0], false) },
}

var hostCheckCmd = &cobra.Command{
	Use:   "check <имя...>",
	Short: "Куда на самом деле резолвится имя",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		_, entries, _ := readHosts()
		for _, name := range args {
			line := ui.Pad(name, 30)
			ips, err := net.LookupHost(name)
			if err != nil {
				line += ui.Bad("не резолвится")
			} else {
				line += strings.Join(ips, " ")
			}
			for _, e := range entries {
				for _, n := range e.Names {
					if n == name {
						state := "через " + hostsFile() + ", профиль " + e.Profile
						if !e.On {
							state = "в " + hostsFile() + " есть, но выключено, профиль " + e.Profile
						}
						line += ui.Dim("  " + state)
					}
				}
			}
			ui.Println(line)
		}
		return nil
	},
}

func init() {
	hostAddCmd.Flags().StringVar(&hostIP, "ip", "127.0.0.1", "адрес, на который указывают имена")
	hostAddCmd.Flags().StringVar(&hostProfile, "profile", hostsfile.DefaultProfile, "профиль записи")
	hostCmd.AddCommand(hostAddCmd, hostRmCmd, hostOnCmd, hostOffCmd, hostCheckCmd)
	root.AddCommand(hostCmd)
}
