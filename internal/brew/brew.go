// Package brew: сервисы и формулы Homebrew.
package brew

import (
	"encoding/json"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

type Service struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	PID      int    `json:"pid"`
	LogPath  string `json:"log_path"`
	ErrPath  string `json:"error_log_path"`
	ExitCode *int   `json:"exit_code"`
}

func ParseServices(data []byte) ([]Service, error) {
	var list []Service
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func Services() ([]Service, error) {
	out, err := runner.Output("brew", "services", "list", "--json")
	if err != nil {
		return nil, err
	}
	return ParseServices([]byte(out))
}

func Info(name string) (Service, error) {
	out, err := runner.Output("brew", "services", "info", name, "--json")
	if err != nil {
		return Service{}, err
	}
	list, err := ParseServices([]byte(out))
	if err != nil || len(list) == 0 {
		return Service{}, err
	}
	return list[0], nil
}

func Prefix() string {
	out, _ := runner.Output("brew", "--prefix")
	return strings.TrimSpace(out)
}

// Formulas: имена установленных формул.
func Formulas() []string {
	out, _ := runner.Output("brew", "list", "--formula", "-1")
	return strings.Fields(out)
}
