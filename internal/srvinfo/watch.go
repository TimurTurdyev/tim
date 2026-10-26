package srvinfo

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/level"
)

// systemDirs: удалённый бинарник отсюда - обновлённый пакет, процесс просто не перезапущен.
var systemDirs = []string{"/usr/", "/bin/", "/sbin/", "/lib/", "/lib64/"}

// Watch: то, что сравнивается с прошлым разом, и признаки взлома.
type Watch struct {
	Keys      map[string][]SSHKey `json:"keys,omitempty"` // пользователь -> ключи; нет пользователя - не прочитан
	Users     []string            `json:"users,omitempty"`
	Units     []string            `json:"units,omitempty"`
	Timers    []string            `json:"timers,omitempty"`
	Cron      []string            `json:"cron,omitempty"`
	CronSpool []string            `json:"cron_spool,omitempty"`
	SpoolRead bool                `json:"spool_read"`
	Suspects  []Suspect           `json:"suspects,omitempty"`
	Stale     []string            `json:"stale,omitempty"` // службы со старыми библиотеками или бинарником
	StaleRead bool                `json:"stale_read"`
	Docker    []Published         `json:"docker,omitempty"`
}

// SSHKey: строка authorized_keys по ssh-keygen -lf.
type SSHKey struct {
	Hash    string `json:"hash"`
	Type    string `json:"type"`
	Comment string `json:"comment"`
}

// Suspect: признак взлома. Kind: exe (процесс из временной папки или из памяти), deleted (бинарник
// удалён у живого процесса), preload, miner, fetchsh (крон или юнит качает и выполняет), shellport.
type Suspect struct {
	Kind   string `json:"kind"`
	PID    string `json:"pid,omitempty"`
	Name   string `json:"name,omitempty"`
	Detail string `json:"detail"`
}

// Published: порт, который публикует Docker.
type Published struct {
	Name string `json:"name"`
	Addr string `json:"addr"`
	Port int    `json:"port"`
}

func parseWatch(s map[string][]string) Watch {
	w := Watch{Keys: map[string][]SSHKey{}}
	for _, l := range s["keys"] {
		f := strings.Fields(l)
		switch {
		case len(f) >= 4 && f[0] == "key":
			k := SSHKey{Hash: f[3], Type: strings.Trim(f[len(f)-1], "()")}
			if len(f) > 5 {
				k.Comment = strings.Join(f[4:len(f)-1], " ")
			}
			w.Keys[f[1]] = append(w.Keys[f[1]], k)
		case len(f) == 2 && f[0] == "read":
			if w.Keys[f[1]] == nil {
				w.Keys[f[1]] = []SSHKey{}
			}
		}
	}
	w.Users, w.Units, w.Timers, w.Cron = s["users"], s["enabled"], s["timers"], s["cron"]
	w.CronSpool, w.SpoolRead = s["cronspool"]
	if st, ok := s["stale"]; ok && !slices.Contains(st, "nosudo") {
		w.Stale, w.StaleRead = st, true
	}
	for _, l := range s["suspect"] {
		kind, rest, _ := strings.Cut(l, " ")
		sp := Suspect{Kind: kind, Detail: rest}
		switch kind {
		case "exe":
			f := strings.SplitN(rest, " ", 3)
			if len(f) < 3 {
				continue
			}
			sp.PID, sp.Name, sp.Detail = f[0], f[1], f[2]
			if path, ok := strings.CutSuffix(sp.Detail, " (deleted)"); ok && !tempPath(path) {
				if slices.ContainsFunc(systemDirs, func(d string) bool { return strings.HasPrefix(path, d) }) {
					w.Stale = append(w.Stale, sp.Name)
					continue
				}
				sp.Kind = "deleted"
			}
		case "miner":
			if f := strings.Fields(rest); len(f) >= 3 {
				sp.PID, sp.Name, sp.Detail = f[0], f[2], strings.Join(f[3:], " ")+" · CPU "+f[1]+"%"
			}
		case "shellport":
			if m := procName.FindStringSubmatch(rest); m != nil {
				sp.Name = m[1]
			}
			if f := strings.Fields(rest); len(f) >= 4 {
				sp.Detail = f[3]
			}
		}
		w.Suspects = append(w.Suspects, sp)
	}
	slices.Sort(w.Stale)
	w.Stale = slices.Compact(w.Stale)
	for _, l := range s["docker"] {
		name, ports, ok := strings.Cut(l, "|")
		if !ok {
			continue
		}
		for _, p := range strings.Split(ports, ",") {
			hostPart, _, ok := strings.Cut(strings.TrimSpace(p), "->")
			if !ok {
				continue
			}
			i := strings.LastIndex(hostPart, ":")
			port, err := strconv.Atoi(hostPart[i+1:])
			if i < 0 || err != nil {
				continue
			}
			pub := Published{Name: name, Addr: strings.Trim(hostPart[:i], "[]"), Port: port}
			if !slices.ContainsFunc(w.Docker, func(x Published) bool { return x.Name == pub.Name && x.Port == pub.Port }) {
				w.Docker = append(w.Docker, pub)
			}
		}
	}
	return w
}

func tempPath(p string) bool {
	return strings.HasPrefix(p, "/tmp/") || strings.HasPrefix(p, "/dev/shm/") || strings.HasPrefix(p, "/var/tmp/") ||
		strings.HasPrefix(p, "/memfd:")
}

// watch: признаки взлома, службы со старыми библиотеками и порты Docker мимо ufw.
func (f *findings) watch(r Report) {
	for _, s := range r.Watch.Suspects {
		switch s.Kind {
		case "exe":
			why := "запущен из временной папки"
			if strings.HasSuffix(s.Detail, "(deleted)") {
				why = "бинарник удалён после запуска - так прячутся майнеры и бэкдоры"
			}
			if strings.HasPrefix(s.Detail, "/memfd:") {
				why = "запущен прямо из памяти, без файла на диске"
			}
			f.add("процесс", level.Bad, s.Name, fmt.Sprintf("%s · pid %s · %s · %s", s.Name, s.PID, s.Detail, why), fixIntruder(s.PID))
		case "deleted":
			f.add("процесс", level.Warn, s.Name, fmt.Sprintf("%s · pid %s · бинарник %s удалён, процесс живёт", s.Name, s.PID, s.Detail),
				"если обновлял сам - перезапусти; если нет: "+fixIntruder(s.PID))
		case "preload":
			f.add("ld.so.preload", level.Bad, s.Detail, "/etc/ld.so.preload подгружает "+s.Detail+" во все программы - так прячут процессы и файлы",
				"если не ставил сам - сервер скомпрометирован: не верь ps и ls на нём, поднимай новый из бэкапа")
		case "miner":
			f.add("майнер", level.Bad, s.Name, fmt.Sprintf("%s · pid %s · %s", s.Name, s.PID, s.Detail), fixIntruder(s.PID))
		case "fetchsh":
			f.add("автозапуск", level.Warn, s.Detail, s.Detail+" качает скрипт из сети и выполняет его (curl | sh или base64 -d)",
				"посмотри, что и откуда качается; не ставил сам - убери и ищи, как попало")
		case "shellport":
			f.add("порт", level.Bad, s.Name+" "+s.Detail, s.Name+" слушает "+s.Detail+" - оболочка на порту - это дверь внутрь",
				"ss -tlnp, кто запустил: ps -fp <pid>; не твоё - останови и ищи, как попало")
		}
	}
	if r.UFW == "active" {
		for _, p := range r.Watch.Docker {
			if p.Addr == "0.0.0.0" || p.Addr == "::" || p.Addr == "" {
				f.add("порт", level.Warn, fmt.Sprintf("%d %s", p.Port, p.Name),
					fmt.Sprintf("docker публикует %s:%d (%s), ufw его не закрывает: Docker пишет правила в обход ufw", p.Addr, p.Port, p.Name),
					fmt.Sprintf("публикуй как 127.0.0.1:%d:%d или закрой в цепочке DOCKER-USER", p.Port, p.Port))
			}
		}
	}
}

func fixIntruder(pid string) string {
	return "кто запустил: ps -fp " + pid + ", ls -l /proc/" + pid + "/exe; не твоё - останови, ищи вход (кроны, юниты, ключи) и смени пароли"
}

// staleText: службы со старыми библиотеками - к находке о перезагрузке.
func staleText(r Report) string {
	if len(r.Watch.Stale) == 0 {
		return ""
	}
	list := r.Watch.Stale[:min(5, len(r.Watch.Stale))]
	text := "не перезапущены " + strings.Join(list, ", ")
	if n := len(r.Watch.Stale) - len(list); n > 0 {
		text += fmt.Sprintf(" и ещё %d", n)
	}
	return text
}
