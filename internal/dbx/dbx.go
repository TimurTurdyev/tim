// Package dbx: база проекта по его .env - клиент, дамп и восстановление.
package dbx

import (
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Conn: как подключиться к базе. Пароль в вывод не попадает: клиенту он уходит через окружение.
type Conn struct {
	Driver, Host, Port, Name, User, Pass string
}

// Load читает параметры базы из .env проекта и дополняет их значениями по умолчанию.
func Load(dir string) (*Conn, error) {
	vals, err := envfile.Parse(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, ui.Errorf("нет .env в %s", dir)
	}
	c := &Conn{Driver: vals["DB_CONNECTION"], Host: vals["DB_HOST"], Port: vals["DB_PORT"], Name: vals["DB_DATABASE"],
		User: vals["DB_USERNAME"], Pass: vals["DB_PASSWORD"]}
	if c.Driver == "" {
		c.Driver = "sqlite"
	}
	if c.Host == "" {
		c.Host = "127.0.0.1"
	}
	if c.User == "" {
		c.User = "root"
	}
	switch c.Driver {
	case "mysql", "mariadb":
		if c.Port == "" {
			c.Port = "3306"
		}
	case "pgsql":
		if c.Port == "" {
			c.Port = "5432"
		}
	case "sqlite":
		if c.Name == "" {
			c.Name = filepath.Join(dir, "database", "database.sqlite")
		} else if !filepath.IsAbs(c.Name) {
			c.Name = filepath.Join(dir, c.Name)
		}
	}
	if c.Name == "" {
		return nil, ui.Errorf("в .env нет DB_DATABASE")
	}
	return c, nil
}

// Client: окружение, программа и аргументы для mode - shell, dump или restore.
func (c *Conn) Client(mode string) (env []string, name string, args []string) {
	switch c.Driver {
	case "mysql", "mariadb":
		env = []string{"MYSQL_PWD=" + c.Pass}
		base := []string{"-h", c.Host, "-P", c.Port, "-u", c.User, c.Name}
		if mode == "dump" {
			return env, "mysqldump", append([]string{"--single-transaction", "--quick"}, base...)
		}
		return env, "mysql", base
	case "pgsql":
		env = []string{"PGPASSWORD=" + c.Pass}
		base := []string{"-h", c.Host, "-p", c.Port, "-U", c.User, c.Name}
		switch mode {
		case "dump":
			return env, "pg_dump", append([]string{"--no-owner", "--no-privileges"}, base...)
		case "restore":
			return env, "psql", append([]string{"-q"}, base...)
		}
		return env, "psql", base
	}
	if mode == "dump" {
		return nil, "sqlite3", []string{c.Name, ".dump"}
	}
	return nil, "sqlite3", []string{c.Name}
}

// Dump снимает дамп в out (.sql.gz). Недописанный файл удаляется: его легко принять за бэкап.
func Dump(c *Conn, out string) error {
	env, bin, args := c.Client("dump")
	if runner.DryRun {
		return runner.Stream(env, nil, nil, "| gzip > "+out, bin, args...)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	gz := gzip.NewWriter(f)
	err = runner.Stream(env, nil, gz, "", bin, args...)
	if cerr := gz.Close(); err == nil {
		err = cerr
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(out)
		return ui.Errorf("дамп не снят: %v", err)
	}
	return nil
}

// Restore заливает дамп (.sql или .sql.gz) в базу.
func Restore(c *Conn, file string) error {
	env, bin, args := c.Client("restore")
	if runner.DryRun {
		return runner.Stream(env, nil, nil, "< "+file, bin, args...)
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var in io.Reader = f
	if strings.HasSuffix(file, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return ui.Errorf("%s: не gzip: %v", file, err)
		}
		defer func() { _ = gz.Close() }()
		in = gz
	}
	return runner.Stream(env, in, os.Stdout, "", bin, args...)
}
