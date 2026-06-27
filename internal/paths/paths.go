// Package paths: где tim хранит своё состояние - кэши, журналы, сокеты.
package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// State: TIM_STATE_DIR, иначе $XDG_STATE_HOME/tim, иначе ~/.local/state/tim; sub - путь внутри.
func State(sub ...string) string {
	base := os.Getenv("TIM_STATE_DIR")
	if base == "" {
		if x := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(x) {
			base = filepath.Join(x, "tim")
		} else {
			base = filepath.Join(Home(), ".local", "state", "tim")
		}
	}
	return filepath.Join(append([]string{base}, sub...)...)
}

// SystemState: состояние служб tim под root - их видят все пользователи, а домашней папки у них нет.
func SystemState() string {
	if runtime.GOOS == "darwin" {
		return "/var/db/tim"
	}
	return "/var/lib/tim"
}

// OwnState: каталог состояния того, кто запущен: под root - системный, иначе пользователя.
func OwnState() string {
	if os.Geteuid() == 0 {
		return SystemState()
	}
	return State()
}

// Home: домашняя папка. У служб systemd HOME не задан, и тогда она берётся из базы пользователей,
// иначе пути вышли бы относительными - от корня диска.
func Home() string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	return ""
}
