// Package paths: где tim хранит своё состояние - кэши, журналы, сокеты.
package paths

import (
	"os"
	"path/filepath"
)

// State: TIM_STATE_DIR, иначе $XDG_STATE_HOME/tim, иначе ~/.local/state/tim; sub - путь внутри.
func State(sub ...string) string {
	base := os.Getenv("TIM_STATE_DIR")
	if base == "" {
		if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
			base = filepath.Join(x, "tim")
		} else {
			home, _ := os.UserHomeDir()
			base = filepath.Join(home, ".local", "state", "tim")
		}
	}
	return filepath.Join(append([]string{base}, sub...)...)
}
