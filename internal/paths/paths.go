// Package paths: где tim хранит своё состояние (кэши, журналы, сокеты) и большие данные.
package paths

import (
	"os"
	"os/user"
	"path/filepath"
	"runtime"
)

// State: TIM_STATE_DIR, иначе $XDG_STATE_HOME/tim, иначе ~/.local/state/tim; sub - путь внутри.
func State(sub ...string) string {
	return filepath.Join(append([]string{base("TIM_STATE_DIR", "XDG_STATE_HOME", "state")}, sub...)...)
}

// Data: большие данные (таблицы сетей, архив имён) - TIM_DATA_DIR, иначе $XDG_DATA_HOME/tim, иначе
// ~/.local/share/tim; sub - путь внутри.
func Data(sub ...string) string {
	return filepath.Join(append([]string{base("TIM_DATA_DIR", "XDG_DATA_HOME", "share")}, sub...)...)
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

// base: каталог tim из своей переменной, иначе из переменной XDG, иначе ~/.local/<kind>/tim.
func base(own, xdg, kind string) string {
	if dir := os.Getenv(own); dir != "" {
		return dir
	}
	if dir := os.Getenv(xdg); filepath.IsAbs(dir) {
		return filepath.Join(dir, "tim")
	}
	return filepath.Join(Home(), ".local", kind, "tim")
}
