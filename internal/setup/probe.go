package setup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/run"
)

// probeFiles: что смотреть в папке проекта, чтобы понять, чего ему не хватает.
var probeFiles = []string{
	".env", ".env.example", ".env.sample", ".env.dist", "composer.json", "vendor",
	"package.json", "node_modules", "go.mod", "uv.lock", "pyproject.toml",
	"requirements.txt", ".venv", "venv", "storage", "public/storage",
}

// Probe: что уже есть в папке проекта - стек, файлы, .env - для построения плана.
func Probe(dir string) State {
	f := run.Detect(dir)
	s := State{Stack: f.Stack, PkgManager: f.PkgManager, Has: map[string]bool{}, Env: map[string]string{}}
	for _, name := range probeFiles {
		if exists(filepath.Join(dir, name)) {
			s.Has[name] = true
		}
	}
	if env, err := envfile.Parse(filepath.Join(dir, ".env")); err == nil {
		s.Env = env
	}
	// Файл базы sqlite проверяем по тому пути, который назван в .env.
	if db := s.Env["DB_DATABASE"]; strings.HasSuffix(db, ".sqlite") && !strings.HasPrefix(db, "/") && exists(filepath.Join(dir, db)) {
		s.Has[db] = true
	}
	if exists(filepath.Join(dir, "database", "database.sqlite")) {
		s.Has["database/database.sqlite"] = true
	}
	return s
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
