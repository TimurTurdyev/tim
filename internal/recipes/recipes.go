// Package recipes: свои заготовки из конфига ([[recipe]]) и задачи проекта - цели Makefile, justfile,
// Taskfile, scripts в package.json и composer.json. Палитра показывает их группой экрана.
package recipes

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/shellq"
)

// Где показывается своя заготовка.
const (
	OnServer  = "server"
	OnProject = "project"
	OnMachine = "machine"
)

// Own: свои заготовки для места on; без on в конфиге заготовка - для сервера.
func Own(cfg *config.Config, on string) []config.Recipe {
	var out []config.Recipe
	for _, r := range cfg.Recipes {
		where := r.On
		if where == "" {
			where = OnServer
		}
		if where == on && r.Name != "" && r.Run != "" {
			out = append(out, r)
		}
	}
	return out
}

// Find: своя заготовка по имени для места on.
func Find(cfg *config.Config, on, name string) (config.Recipe, bool) {
	i := slices.IndexFunc(Own(cfg, on), func(r config.Recipe) bool { return r.Name == name })
	if i < 0 {
		return config.Recipe{}, false
	}
	return Own(cfg, on)[i], true
}

// Expand: {server} {project} {path} {port} {unit} - значения в кавычках оболочки; чего нет в vars,
// остаётся как написано.
func Expand(run string, vars map[string]string) string {
	for k, v := range vars {
		run = strings.ReplaceAll(run, "{"+k+"}", shellq.Quote(v))
	}
	return run
}

// Свои команды tim проверить на "только чтение" не может: перед первым запуском спрашивает,
// согласие помнит по хэшу команды - правка команды спросит заново.
func approvedPath() string { return paths.State("recipes-approved") }

func sum(script string) string {
	h := sha256.Sum256([]byte(script))
	return hex.EncodeToString(h[:])
}

// Approved: на эту команду уже соглашались.
func Approved(script string) bool {
	data, _ := os.ReadFile(approvedPath())
	return slices.Contains(strings.Fields(string(data)), sum(script))
}

// Approve запоминает согласие.
func Approve(script string) error {
	if Approved(script) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(approvedPath()), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(approvedPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteString(sum(script) + "\n")
	return err
}
