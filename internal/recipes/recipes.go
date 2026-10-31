// Package recipes: свои заготовки сервера из конфига ([[recipe]]) и согласие на их запуск.
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

// Own: свои заготовки сервера; on = "project" и "machine" запускал tim run, их больше нет.
func Own(cfg *config.Config) []config.Recipe {
	var out []config.Recipe
	for _, r := range cfg.Recipes {
		if (r.On == "" || r.On == "server") && r.Name != "" && r.Run != "" {
			out = append(out, r)
		}
	}
	return out
}

// Expand: {server} {unit} - значения в кавычках оболочки; чего нет в vars, остаётся как написано.
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
