// Package fill: поля реестра, которые можно узнать из самого проекта.
package fill

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/envfile"
)

// Found: что удалось узнать о проекте.
type Found struct {
	URL  string
	Port int
	DB   string
}

// local: адрес не указывает на сайт в интернете.
// Local: адрес живёт только на этой машине, в реестр и во внешние проверки он не годится.
func Local(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "0.0.0.0", "::1", "host.docker.internal":
		return true
	}
	return strings.HasSuffix(host, ".test") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".localhost")
}

// FromEnv достаёт адрес сайта, порт и базу из .env проекта.
func FromEnv(dir string) Found {
	vals, err := envfile.Parse(filepath.Join(dir, ".env"))
	if err != nil {
		return Found{}
	}
	var f Found
	if raw := strings.TrimSpace(vals["APP_URL"]); raw != "" {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			if p := u.Port(); p != "" {
				f.Port, _ = strconv.Atoi(p)
			}
			if !Local(u.Hostname()) {
				f.URL = u.Scheme + "://" + u.Host
			}
		}
	}
	f.DB = db(vals, dir)
	return f
}

func db(vals map[string]string, dir string) string {
	conn := strings.ToLower(strings.TrimSpace(vals["DB_CONNECTION"]))
	name := strings.TrimSpace(vals["DB_DATABASE"])
	if conn == "" && name == "" {
		return ""
	}
	if conn == "sqlite" {
		if name == "" {
			return "sqlite"
		}
		// В Laravel путь к файлу бывает абсолютным: в реестре держим его относительно проекта.
		if rel, err := filepath.Rel(dir, name); err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		return "sqlite:" + name
	}
	if name == "" {
		return ""
	}
	if conn == "" {
		return name
	}
	return conn + ":" + name
}
