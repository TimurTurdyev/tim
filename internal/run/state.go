package run

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"

	"github.com/TimurTurdyev/tim/internal/procs"
)

// State: фоновый dev-сервер, запущенный tim serve -d.
type State struct {
	Name    string    `json:"name"`
	PID     int       `json:"pid"`
	Port    string    `json:"port"`
	Dir     string    `json:"dir"`
	Command string    `json:"command"`
	Log     string    `json:"log"`
	Started time.Time `json:"started"`
}

func StateDir() string {
	d := paths.State("serve")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func LogPath(name string) string { return filepath.Join(StateDir(), name+".log") }

func (s State) Save() error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(StateDir(), s.Name+".json"), data, 0o600)
}

func (s State) Remove() { _ = os.Remove(filepath.Join(StateDir(), s.Name+".json")) }

// Alive: процесс ещё есть и это наш (сигнал 0 ничего не делает, только проверяет).
// Процесс, запущенный позже записи, - чужой, получивший тот же PID после перезагрузки.
func (s State) Alive() bool {
	if s.PID <= 0 {
		return false
	}
	if err := syscall.Kill(s.PID, 0); err != nil && err != syscall.EPERM {
		return false
	}
	if at, ok := procs.Started(s.PID); ok && !s.Started.IsZero() && at.After(s.Started.Add(5*time.Second)) {
		return false
	}
	return true
}

func LoadState(name string) (State, bool) {
	data, err := os.ReadFile(filepath.Join(StateDir(), name+".json"))
	if err != nil {
		return State{}, false
	}
	var s State
	if json.Unmarshal(data, &s) != nil {
		return State{}, false
	}
	return s, true
}

func ListStates() []State {
	files, _ := filepath.Glob(filepath.Join(StateDir(), "*.json"))
	var out []State
	for _, f := range files {
		if s, ok := LoadState(strings.TrimSuffix(filepath.Base(f), ".json")); ok {
			out = append(out, s)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
