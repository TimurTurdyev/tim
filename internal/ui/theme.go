package ui

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/paths"
)

var (
	lightOnce sync.Once
	light     atomic.Bool
)

// Light: фон терминала светлый - красить светлой палитрой. Терминал здесь не спрашивается: вопрос
// стоит обращения к нему и съедает набранное заранее, а ui зовут и из хука оболочки на каждый cd.
// Порядок: TIM_THEME, COLORFGBG, фон, который экран tim узнал в прошлый раз, иначе тёмный.
func Light() bool {
	lightOnce.Do(func() { light.Store(detectLight()) })
	return light.Load()
}

// SetLight: фон узнал экран у самого терминала - это точнее догадок.
func SetLight(v bool) {
	lightOnce.Do(func() {})
	light.Store(v)
}

// Palette: цвета под текущий фон.
func Palette() brand.Palette {
	if Light() {
		return brand.Light
	}
	return brand.Dark
}

// BackgroundFile: где лежит фон, который экран узнал у этой программы-терминала; внутри light или dark.
func BackgroundFile() string {
	name := strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
			return r
		}
		return '_'
	}, os.Getenv("TERM_PROGRAM"))
	if name == "" {
		name = "default"
	}
	return paths.State("background", name)
}

// ThemeFixed: тему задали руками через TIM_THEME - ответ терминала её не меняет.
func ThemeFixed() bool {
	v := os.Getenv("TIM_THEME")
	return v == "light" || v == "dark"
}

func detectLight() bool {
	if ThemeFixed() {
		return os.Getenv("TIM_THEME") == "light"
	}
	if v, ok := parseColorFGBG(os.Getenv("COLORFGBG")); ok {
		return v
	}
	data, err := os.ReadFile(BackgroundFile())
	return err == nil && strings.TrimSpace(string(data)) == "light"
}

// parseColorFGBG: "15;0" или "0;default;15" - последнее число это фон из 16 цветов терминала;
// светлые среди них 7 и 9..15.
func parseColorFGBG(s string) (isLight, ok bool) {
	parts := strings.Split(s, ";")
	bg, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || len(parts) < 2 || bg < 0 || bg > 15 {
		return false, false
	}
	return bg == 7 || bg >= 9, true
}
