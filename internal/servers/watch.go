package servers

import (
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// kindWords: как назвать вид в "запомнил 14 ключей, 9 портов".
var (
	kindWords = map[string][3]string{
		"key": {"ключ ssh", "ключа ssh", "ключей ssh"}, "user": {"пользователя", "пользователей", "пользователей"},
		"port": {"порт", "порта", "портов"}, "unit": {"юнит", "юнита", "юнитов"}, "timer": {"таймер", "таймера", "таймеров"},
		"cron": {"строку крона", "строки крона", "строк крона"},
	}
	kindOrder = []string{"key", "user", "port", "unit", "timer", "cron"}
)

// object: сервер в хранилище "своего" - по хосту, как и кэш: один сервер под разными алиасами один.
func object(target string) string { return "server-" + hostKey(target) }

// snapshot: что на сервере есть сейчас; чего не удалось прочитать, в снимке нет.
func snapshot(r srvinfo.Report) baseline.Snapshot {
	snap := baseline.Snapshot{}
	for user, keys := range r.Watch.Keys {
		kind := "key:" + user
		snap[kind] = []baseline.Item{}
		for _, k := range keys {
			snap[kind] = append(snap[kind], baseline.Item{Key: k.Hash, Text: strings.TrimSpace(k.Type + " " + quoted(k.Comment))})
		}
	}
	add := func(kind string, keys []string) {
		if keys == nil && kind != "port" {
			return
		}
		snap[kind] = []baseline.Item{}
		for _, k := range keys {
			snap[kind] = append(snap[kind], baseline.Item{Key: k})
		}
	}
	var ports []string
	for _, l := range r.Listening {
		if l.Public {
			ports = append(ports, l.Addr+":"+strconv.Itoa(l.Port))
		}
	}
	add("port", ports)
	add("user", r.Watch.Users)
	add("unit", r.Watch.Units)
	add("timer", r.Watch.Timers)
	add("cron", r.Watch.Cron)
	if r.Outside != nil && r.Outside.HostKey != "" && !r.Outside.Changed {
		snap["hostkey"] = []baseline.Item{{Key: r.Outside.HostKey, Text: "ключ хоста " + r.Outside.HostKey}}
	}
	if r.Watch.SpoolRead {
		add("cron-spool", append([]string{}, r.Watch.CronSpool...))
	}
	return snap
}

func quoted(s string) string {
	if s == "" {
		return ""
	}
	return `"` + s + `"`
}

// observe сравнивает отчёт с прошлым разом; при первом сборе отдаёт "запомнил ...".
func observe(target string, r srvinfo.Report) string {
	res := baseline.Observe(object(target), snapshot(r), time.Now())
	first := map[string]int{}
	for kind, n := range res.First {
		base, _, _ := strings.Cut(kind, ":")
		base = strings.TrimSuffix(base, "-spool")
		first[base] += n
	}
	return baseline.FirstText(first, kindWords, kindOrder)
}

// newWhat: что это - для колонки ЧТО.
func newWhat(it baseline.Item) string {
	kind, user, _ := strings.Cut(it.Kind, ":")
	switch kind {
	case "key":
		return "ключ ssh " + user
	case "user":
		return "пользователь"
	case "port":
		return "порт " + it.Key[strings.LastIndex(it.Key, ":")+1:]
	case "unit":
		return "юнит"
	case "timer":
		return "таймер"
	case "hostkey":
		return "ключ хоста"
	case "suid":
		return "SUID-файл"
	case "login-net":
		return "вход из новой сети"
	}
	return "крон"
}

// newText: строка нового - что и когда появилось.
func newText(it baseline.Item) string {
	text := it.Text
	if text == "" {
		text = it.Key
	}
	seen := it.Seen.Local()
	when := "появилось к " + seen.Format("02.01 15:04")
	if after := it.After.Local(); !it.After.IsZero() {
		end := seen.Format("02.01 15:04")
		if after.YearDay() == seen.YearDay() {
			end = seen.Format("15:04")
		}
		when = "появилось между " + after.Format("02.01 15:04") + " и " + end
	}
	return text + " " + "·" + " " + when
}

// newFix: что делать с новым, если это не ты.
func newFix(target string, it baseline.Item) string {
	kind, user, _ := strings.Cut(it.Kind, ":")
	switch kind {
	case "key":
		return "не твой - убери из ~" + user + "/.ssh/authorized_keys, смени пароли, смотри tim server " + target + " --attacks"
	case "user":
		return "не заводил - проверь /etc/passwd и кто входил: tim server " + target + " --attacks"
	case "port":
		return "не открывал - кто слушает: tim server " + target + " --run ports"
	case "unit", "timer":
		return "не ставил - что запускает: systemctl cat " + it.Key
	case "login-net":
		return "не ты - смени пароль и ключи, смотри tim server " + target + " --attacks"
	case "suid":
		return "setuid даёт права владельца любому: чей файл - dpkg -S " + it.Key + "; не из пакета - убери бит chmod u-s"
	case "hostkey":
		return "сервер не переустанавливал - ключ хоста не должен меняться: не входи и выясни, кто посередине"
	}
	return "не ставил - что и кто запускает: tim server " + target + " --run cron"
}

// printNew: новое с прошлого раза строками отчёта.
func printNew(target, remembered string) {
	if remembered != "" {
		ui.Println("  " + ui.Dim(remembered))
	}
	items := baseline.Pending(object(target))
	if len(items) == 0 {
		return
	}
	ui.Println(ui.Warn("новое с " + baseline.Since(items).Local().Format("02.01 15:04") + ":"))
	for _, it := range items {
		ui.Println("  " + ui.Pad(newWhat(it), 22) + newText(it))
	}
	ui.Println("  " + ui.Dim("своё - tim server "+target+" --accept; не твоё - что делать, покажет экран tim server "+target))
}

// acceptNew: всё новое сервера - своё.
func acceptNew(target string) error {
	n := len(baseline.Pending(object(target)))
	if n == 0 {
		ui.Println(ui.Dim(target + ": нового с прошлого раза нет"))
		return nil
	}
	if err := baseline.Accept(object(target), "", ""); err != nil {
		return err
	}
	ui.Println(ui.Label(target) + ": отметил своим " + strconv.Itoa(n) + ", больше не новое")
	return nil
}
