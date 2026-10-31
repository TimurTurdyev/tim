package machine

import (
	"cmp"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/stack"
	"github.com/TimurTurdyev/tim/internal/sysinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// ForgottenAfter: dev-сервер, который работает дольше, обычно забыли остановить.
const ForgottenAfter = 8 * time.Hour

// PortRow: процесс, который слушает порты, и чей он.
type PortRow struct {
	netinfo.Listener // Port - первый из Ports
	procs.Proc
	Ports     []string // все порты процесса по возрастанию
	Folder    string   // имя папки, где запущен: корень git-репозитория или сама папка
	Dir       string   // её путь
	Kind      string   // что это: Vite, PostgreSQL, имя контейнера
	Container *sysinfo.Container
	Agent     *procs.Agent // процесс поднял агент; есть только в PortsFull
}

type portsOptions struct {
	all, old, kill, force, json bool
	older                       time.Duration
}

// portJSON: строка порта для --json - то, что видно в таблице и карточке, без внутренних полей.
type portJSON struct {
	Ports     []int              `json:"ports"`
	Kind      string             `json:"kind,omitempty"`
	Folder    string             `json:"folder,omitempty"`
	Dir       string             `json:"dir,omitempty"`
	Agent     *procs.Agent       `json:"agent,omitempty"`
	Forgotten bool               `json:"forgotten"`
	PID       int                `json:"pid,omitempty"`
	Uptime    int64              `json:"uptime_seconds,omitempty"`
	Memory    int64              `json:"memory_bytes,omitempty"`
	Command   string             `json:"command,omitempty"`
	Cwd       string             `json:"cwd,omitempty"`
	Container *sysinfo.Container `json:"container,omitempty"`
}

// Ports: кто слушает TCP-порты и в какой папке запущен; порты одного процесса - одной строкой, по номеру.
// Без имён контейнеров и агентов: docker ps и окружение процессов дороги для сводки, которая идёт в каждом
// новом терминале.
func Ports(cfg *config.Config) []PortRow { return merge(listeners(false)) }

// PortsFull: то же с именами контейнеров на проброшенных портах и агентами, которые подняли процессы.
func PortsFull(cfg *config.Config) []PortRow { return merge(listeners(true)) }

// listeners: строка на каждый слушающий порт.
func listeners(full bool) []PortRow {
	ls := netinfo.Listening()
	// Один процесс слушает несколько портов, чужие - без pid: ps и lsof зовутся по уникальным.
	var pids []string
	for _, l := range ls {
		if l.PID != "" && !slices.Contains(pids, l.PID) {
			pids = append(pids, l.PID)
		}
	}
	info := procs.Info(pids)
	rows := make([]PortRow, 0, len(ls))
	for _, l := range ls {
		r := PortRow{Listener: l, Proc: info[l.PID]}
		if r.Proc.Command == "" {
			r.Proc.Command = l.Command
		}
		// Приложения из .app стартуют в папке, откуда их открыли, это не делает их частью папки. Python
		// из brew тоже запускается из Python.app, но внутри Python.framework - это обычная программа.
		if !strings.Contains(r.Proc.Command, ".app/Contents/") || strings.Contains(r.Proc.Command, ".framework/") {
			r.Dir = folderOf(r.Cwd)
			if r.Dir != "" {
				r.Folder = filepath.Base(r.Dir)
			}
		}
		r.Kind = kindOf(r)
		rows = append(rows, r)
	}
	if !full {
		return rows
	}
	agents := procs.Agents(pids)
	for i := range rows {
		if a, ok := agents[rows[i].Listener.PID]; ok {
			rows[i].Agent = &a
		}
	}
	if slices.ContainsFunc(rows, func(r PortRow) bool { return sysinfo.DockerProxy(r.Proc.Command) }) {
		byPort := sysinfo.Containers()
		for i := range rows {
			port, _ := strconv.Atoi(rows[i].Port)
			if c, ok := byPort[port]; ok {
				rows[i].Container, rows[i].Kind = &c, c.Name
			}
		}
	}
	return rows
}

// merge: порты одного процесса - одной строкой; у Docker один процесс держит порты разных контейнеров,
// поэтому склеиваются только порты одного контейнера. Строки - по номеру первого порта.
func merge(rows []PortRow) []PortRow {
	var out []PortRow
	at := map[string]int{}
	for _, r := range rows {
		key := r.Listener.PID + " " + r.Kind
		if i, ok := at[key]; ok && r.Listener.PID != "" {
			out[i].Ports = append(out[i].Ports, r.Port)
			out[i].Network = out[i].Network || r.Network
			continue
		}
		at[key] = len(out)
		r.Ports = []string{r.Port}
		out = append(out, r)
	}
	for i := range out {
		slices.SortFunc(out[i].Ports, func(a, b string) int { return cmp.Compare(portNum(a), portNum(b)) })
		out[i].Port = out[i].Ports[0]
	}
	slices.SortStableFunc(out, func(a, b PortRow) int {
		return cmp.Or(cmp.Compare(portNum(a.Port), portNum(b.Port)), cmp.Compare(a.Listener.PID, b.Listener.PID))
	})
	return out
}

// folderOf: папка, где запущен процесс, - корень git-репозитория над cwd, иначе сама cwd внутри домашней
// папки. Домашняя папка, всё вне её, скрытые папки (~/.gradle, ~/.nvm) и ~/Library - не папка работы.
func folderOf(cwd string) string {
	home := paths.Home()
	rel, err := filepath.Rel(home, cwd)
	if err != nil || strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") || rel == "Library" || strings.HasPrefix(rel, "Library/") {
		return ""
	}
	for d := cwd; d != home && d != "/"; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return d
		}
	}
	return cwd
}

// devServer: процесс, запущенный в папке работы, - его видимость в сети стоит показывать; системные
// службы macOS (AirPlay, общий доступ) слушают все адреса по своей природе.
func devServer(r PortRow) bool { return r.Dir != "" }

// Forgotten: процесс, который, похоже, забыли: сессия агента закрыта; или агент молчит, а процесс
// работает дольше limit; или процесс в папке работы, запущенный вручную, работает дольше limit.
func (r PortRow) Forgotten(limit time.Duration) bool {
	switch {
	case r.Agent != nil && !r.Agent.Alive:
		return true
	case r.Agent != nil:
		return r.Uptime >= limit && time.Since(r.Agent.Active) >= limit
	}
	return r.Dir != "" && r.Uptime >= limit
}

// kindOf: что это за процесс - известный сервер, иначе стек проекта, иначе понятное имя процесса.
func kindOf(r PortRow) string {
	if s := procs.Service(r.Proc.Command, r.Args); s != "" {
		return s
	}
	// ssh держит порт только пробросом: -L, -D или общее соединение с ними ("ssh: ... [mux]"). Раньше
	// стека папки: проброс из папки Laravel - не Laravel.
	if c := r.Proc.Command; filepath.Base(c) == "ssh" || strings.HasPrefix(c, "ssh: ") {
		return "ssh-туннель"
	}
	if r.Dir != "" {
		if s := stack.Name(r.Dir); s != "" {
			return s
		}
	}
	if r.Proc.Command == "" {
		return ""
	}
	return procs.Name(r.Proc.Command, r.Args)
}

func portNum(p string) int {
	n, _ := strconv.Atoi(p)
	return n
}

// Forgotten: забытые процессы из списка - см. PortRow.Forgotten.
func Forgotten(rows []PortRow, limit time.Duration) []PortRow {
	var out []PortRow
	for _, r := range rows {
		if r.Forgotten(limit) {
			out = append(out, r)
		}
	}
	return out
}

func printPorts(cfg *config.Config, o portsOptions) error {
	if o.old {
		return printForgotten(cfg, o)
	}
	var rows []PortRow
	for _, r := range PortsFull(cfg) {
		port, _ := strconv.Atoi(r.Port)
		if !o.all && r.Dir == "" && r.Agent == nil && (procs.System(r.Proc.Command) || port > 49151) {
			continue
		}
		rows = append(rows, r)
	}
	if o.json {
		return printJSON(rows)
	}
	printRows(rows)
	tail := ui.Count(len(rows), "порт", "порта", "портов")
	if !o.all {
		tail += " \u00b7 системные и эфемерные скрыты: --all"
	}
	ui.Println(ui.Dim(tail + " \u00b7 карточка и kill: tim port <n> [kill]"))
	return nil
}

func printForgotten(cfg *config.Config, o portsOptions) error {
	rows := Forgotten(PortsFull(cfg), o.older)
	if o.json && !o.kill {
		return printJSON(rows)
	}
	if len(rows) == 0 {
		ui.Println(ui.Dim("забытых процессов нет: ни сессий агентов, которые закрылись, ни запущенного старше " + ui.Short(o.older)))
		return nil
	}
	printRows(rows)
	if o.kill {
		killRows(rows, o.force)
		return nil
	}
	ui.Println(ui.Dim("остановить: tim ports --old --kill"))
	return nil
}

// portOwner: кто держит порт; с "kill" вторым словом - остановить его.
func portOwner(cfg *config.Config, args []string, o portsOptions) error {
	port := args[0]
	if _, err := strconv.Atoi(port); err != nil {
		return ui.Errorf("порт должен быть числом: %s", port)
	}
	var found []PortRow
	for _, r := range PortsFull(cfg) {
		if slices.Contains(r.Ports, port) {
			found = append(found, r)
		}
	}
	if len(found) == 0 {
		if o.json {
			return printJSON([]PortRow{})
		}
		ui.Println(ui.Dim("порт " + port + " свободен"))
		return nil
	}
	if o.json && (len(args) < 2 || args[1] != "kill") {
		return printJSON(found)
	}
	if len(args) > 1 && args[1] == "kill" {
		printRows(found)
		killRows(found, o.force)
		return nil
	}
	for _, r := range found {
		printCard(r)
	}
	return nil
}

// killRows: процессу - сигнал; порту контейнера - docker stop, иначе лёг бы весь движок Docker.
// Если сервер тут же поднялся заново, его перезапускает родитель (php artisan serve следит за своим
// php -S) - тогда останавливается и родитель.
func killRows(rows []PortRow, force bool) {
	sig := "-TERM"
	if force {
		sig = "-KILL"
	}
	for _, r := range rows {
		if !runner.Confirm(fmt.Sprintf("остановить %s (pid %s)?", ui.Dash(r.Kind), r.Listener.PID)) {
			continue
		}
		if r.Container != nil {
			if err := runner.Run("docker", "stop", r.Container.Name); err != nil {
				ui.Println(ui.Bad("не вышло: " + err.Error()))
			}
			continue
		}
		if err := runner.Run("kill", sig, r.Listener.PID); err != nil {
			ui.Println(ui.Bad("не вышло: " + err.Error()))
			continue
		}
		if again, ok := respawned(r.Port, r.Listener.PID, 2*time.Second); ok {
			parent := procs.Info([]string{again.PPID})[again.PPID]
			ui.Println(ui.Warn("порт " + r.Port + " поднят заново: " + shortCommand(parent) + " (pid " + again.PPID + ") перезапускает сервер, останавливаю и его"))
			_ = runner.Run("kill", sig, again.PPID)
			_ = runner.Run("kill", sig, again.PID)
		}
	}
}

// respawned: на порту за wait появился новый процесс вместо остановленного. Опрос каждые 300 мс.
func respawned(port, oldPID string, wait time.Duration) (procs.Proc, bool) {
	for end := time.Now().Add(wait); time.Now().Before(end); time.Sleep(300 * time.Millisecond) {
		if l, busy := netinfo.PortBusy(port); busy && l.PID != "" && l.PID != oldPID {
			if p, ok := procs.Info([]string{l.PID})[l.PID]; ok {
				return p, true
			}
		}
	}
	return procs.Proc{}, false
}

func checkPorts(addrs []string) error {
	failed := 0
	for _, a := range addrs {
		d, err := netinfo.Dial(a, inspect.Timeout)
		if err != nil {
			failed++
			ui.Println(ui.Bad(ui.Pad(a, 28)) + " " + ui.Dim(err.Error()))
			continue
		}
		ui.Println(ui.Label(ui.Pad(a, 28)) + fmt.Sprintf(" открыт, %d мс", d.Milliseconds()))
	}
	if failed > 0 {
		return ui.Errorf("закрыто: %d из %d", failed, len(addrs))
	}
	return nil
}

// printJSON: строки портов для скриптов и агентов.
func printJSON(rows []PortRow) error {
	out := make([]portJSON, 0, len(rows))
	for _, r := range rows {
		j := portJSON{Kind: r.Kind, Folder: r.Folder, Dir: r.Dir, Agent: r.Agent, Forgotten: r.Forgotten(ForgottenAfter),
			Uptime: int64(r.Uptime.Seconds()), Memory: r.RSS, Command: cmp.Or(r.Args, r.Proc.Command), Cwd: r.Cwd, Container: r.Container}
		j.PID, _ = strconv.Atoi(r.Listener.PID)
		for _, p := range r.Ports {
			j.Ports = append(j.Ports, portNum(p))
		}
		out = append(out, j)
	}
	return ui.JSON(out)
}

// whoShort: кто запустил - для колонки: "агент", "агент, сессия закрыта"; руками - пусто.
func whoShort(r PortRow) string {
	switch {
	case r.Agent == nil:
		return ""
	case !r.Agent.Alive:
		return "агент, сессия закрыта"
	case !r.Agent.Active.IsZero() && time.Since(r.Agent.Active) >= ForgottenAfter:
		return "агент молчит " + ui.Short(time.Since(r.Agent.Active))
	case r.Agent.Title != "":
		return "агент: " + ui.Clip(r.Agent.Title, 28)
	}
	return "агент"
}

// printRows: колонки по самому длинному значению; команда целиком - в карточке tim port <n>.
func printRows(rows []PortRow) {
	table := [][]string{{"ПОРТ", "ЧТО", "ПАПКА", "КТО", "РАБОТАЕТ", "PID"}}
	for _, r := range rows {
		// Пустая ячейка - пустая: столбик прочерков в девяти строках из десяти - шум.
		kind := r.Kind
		if r.Network && devServer(r) {
			kind += " · в сети"
		}
		table = append(table, []string{strings.Join(r.Ports, ", "), kind, r.Folder, whoShort(r), uptime(r.Uptime), r.Listener.PID})
	}
	widths := make([]int, len(table[0]))
	for _, t := range table {
		for i, c := range t {
			widths[i] = max(widths[i], ui.Width(c))
		}
	}
	for n, t := range table {
		var cells []string
		for i, c := range t {
			switch {
			case i == len(t)-1:
				c = strings.Repeat(" ", max(0, widths[i]-ui.Width(c))) + c // PID - число, вправо
			default:
				c = ui.Pad(c, widths[i])
			}
			cells = append(cells, c)
		}
		if n == 0 {
			ui.Println(ui.Dim(strings.Join(cells, "  ")))
			continue
		}
		ui.Println(ui.Label(cells[0]) + "  " + strings.Join(cells[1:3], "  ") + "  " + ui.Dim(strings.Join(cells[3:], "  ")))
	}
}

func uptime(d time.Duration) string {
	if d == 0 {
		return ""
	}
	return ui.Short(d)
}
