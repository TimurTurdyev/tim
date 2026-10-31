// Package visits: какие команды набирали руками и как давно - для недавнего на главном экране.
package visits

import (
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// keepCommands: сколько последних команд помнить - больше в недавнее всё равно не влезает.
const keepCommands = 30

// Entry: сколько раз набирали и когда последний.
type Entry struct {
	Count int       `json:"count"`
	Last  time.Time `json:"last"`
}

func commandsPath() string {
	return paths.State("commands.json")
}

// Commands: команды tim, набранные руками, - строка без tim и когда.
func Commands() map[string]Entry {
	m, _ := load(commandsPath())
	return m
}

// RecordCommand отмечает набранную команду; помнит keepCommands последних.
func RecordCommand(line string, now time.Time) { record(commandsPath(), line, now, keepCommands) }

func load(file string) (map[string]Entry, error) {
	m := map[string]Entry{}
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(data, &m)
}

// record: keep больше нуля - лишние самые давние выбрасываются.
func record(file, name string, now time.Time, keep int) {
	m, err := load(file)
	if err != nil {
		return
	}
	e := m[name]
	e.Count++
	e.Last = now
	m[name] = e
	for keep > 0 && len(m) > keep {
		oldest := ""
		for k, v := range m {
			if oldest == "" || v.Last.Before(m[oldest].Last) {
				oldest = k
			}
		}
		delete(m, oldest)
	}
	data, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = runner.WriteAtomic(file, data, 0o600)
}
