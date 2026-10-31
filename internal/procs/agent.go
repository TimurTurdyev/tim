package procs

import (
	"bufio"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// Агент оставляет метки в окружении процесса, который запустил: AI_AGENT - кто он и какой версии, номер
// сессии и pid самого агента - открыта ли ещё сессия. По журналу сессии видно, как она называется и когда
// последний раз что-то делала.

var envRe = regexp.MustCompile(`\b(AI_AGENT|CLAUDECODE|CLAUDE_CODE_SESSION_ID|CLAUDE_PID|CLAUDE_CONFIG_DIR)=(\S+)`)

var (
	commandRe = regexp.MustCompile(`<command-name>/?([^<\s]+)\s*</command-name>`)
	argsRe    = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

// sessionCommands: встроенные команды агента, по которым не понять, о чём сессия.
var sessionCommands = []string{"clear", "compact", "resume", "model", "login", "logout", "config", "status",
	"cost", "help", "effort", "fast", "exit", "memory", "permissions"}

// Agent: кто из агентов запустил процесс.
type Agent struct {
	Name    string    `json:"name"`              // AI_AGENT до первого "_", без версии
	Session string    `json:"session,omitempty"` // номер сессии
	Title   string    `json:"title,omitempty"`   // первая просьба в сессии
	Alive   bool      `json:"session_alive"`     // процесс агента ещё работает
	Active  time.Time `json:"session_active"`    // когда сессия последний раз писала в журнал
}

// Agents: агенты процессов по pid; процесса без меток в ответе нет.
func Agents(pids []string) map[string]Agent {
	envs := par.Map(pids, 8, environ)
	out := map[string]Agent{}
	for i, env := range envs {
		m := map[string]string{}
		for _, kv := range envRe.FindAllStringSubmatch(env, -1) {
			m[kv[1]] = kv[2]
		}
		if m["AI_AGENT"] == "" && m["CLAUDECODE"] != "1" {
			continue
		}
		name, _, _ := strings.Cut(m["AI_AGENT"], "_")
		// Без pid агента не понять, закрыта ли сессия: такой процесс забыт только по времени работы.
		pid := m["CLAUDE_PID"]
		a := Agent{Name: cmp.Or(name, "агент"), Session: m["CLAUDE_CODE_SESSION_ID"], Alive: pid == "" || alive(pid)}
		a.Title, a.Active = session(m["CLAUDE_CONFIG_DIR"], a.Session)
		out[pids[i]] = a
	}
	return out
}

// environ: окружение своего процесса. На macOS его отдаёт ps -E после команды, на Linux - /proc.
func environ(pid string) string {
	if runtime.GOOS == "linux" {
		data, _ := os.ReadFile("/proc/" + pid + "/environ")
		return strings.ReplaceAll(string(data), "\x00", " ")
	}
	out, _ := runner.Output("ps", "-E", "-ww", "-o", "command=", "-p", pid)
	return out
}

func alive(pid string) bool {
	if pid == "" {
		return false
	}
	out, err := runner.Output("ps", "-o", "pid=", "-p", pid)
	return err == nil && strings.TrimSpace(out) != ""
}

// session: имя сессии - начало первой просьбы - и время последней записи в её журнал. Журнал лежит
// в <папка агента>/projects/<проект>/<сессия>.jsonl; папка - из окружения, иначе любая скрытая в домашней.
func session(configDir, id string) (string, time.Time) {
	if id == "" {
		return "", time.Time{}
	}
	pattern := filepath.Join(configDir, "projects", "*", id+".jsonl")
	if configDir == "" {
		pattern = filepath.Join(paths.Home(), ".*", "projects", "*", id+".jsonl")
	}
	files, _ := filepath.Glob(pattern)
	if len(files) == 0 {
		return "", time.Time{}
	}
	st, err := os.Stat(files[0])
	if err != nil {
		return "", time.Time{}
	}
	return firstPrompt(files[0]), st.ModTime()
}

// firstPrompt: текст первого сообщения человека в журнале сессии, одной строкой.
func firstPrompt(file string) string {
	f, err := os.Open(file)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 8<<20)
	for n := 0; sc.Scan() && n < 200; n++ {
		var rec struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		// isMeta - вставки самого агента: текст скилла, пометки о локальных командах.
		if json.Unmarshal(sc.Bytes(), &rec) != nil || rec.Type != "user" || rec.IsMeta {
			continue
		}
		if text := promptText(rec.Message.Content); text != "" {
			return text
		}
	}
	return ""
}

// promptText: содержимое бывает строкой или списком частей; служебные вставки в угловых скобках - не
// просьба, кроме слэш-команды: "/release-notes за неделю" и есть то, о чём сессия.
func promptText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		if json.Unmarshal(raw, &parts) != nil {
			return ""
		}
		for _, p := range parts {
			if p.Type != "text" {
				continue
			}
			if s == "" {
				s = p.Text
			}
			if !strings.HasPrefix(strings.TrimSpace(p.Text), "<") {
				s = p.Text
				break
			}
		}
	}
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "<") {
		return strings.Join(strings.Fields(s), " ")
	}
	m := commandRe.FindStringSubmatch(s)
	if m == nil || slices.Contains(sessionCommands, m[1]) {
		return ""
	}
	title := "/" + m[1]
	if a := argsRe.FindStringSubmatch(s); a != nil && strings.TrimSpace(a[1]) != "" {
		title += " " + strings.Join(strings.Fields(a[1]), " ")
	}
	return title
}
