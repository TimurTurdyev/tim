package cmd

import (
	"compress/gzip"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/envfile"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

type dbConn struct {
	Driver, Host, Port, Name, User, Pass string
}

// loadDB читает параметры базы из .env проекта; пароль не печатается.
func loadDB(p *config.Project, dir string) (*dbConn, error) {
	vals, err := envfile.Parse(filepath.Join(dir, ".env"))
	if err != nil {
		return nil, ui.Errorf("%s: нет .env в %s", p.Name, dir)
	}
	c := &dbConn{Driver: vals["DB_CONNECTION"], Host: vals["DB_HOST"], Port: vals["DB_PORT"], Name: vals["DB_DATABASE"], User: vals["DB_USERNAME"], Pass: vals["DB_PASSWORD"]}
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
		return nil, ui.Errorf("%s: в .env нет DB_DATABASE", p.Name)
	}
	return c, nil
}

func (c *dbConn) clientArgs(mode string) (env []string, name string, args []string) {
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
		if mode == "dump" {
			return env, "pg_dump", append([]string{"--no-owner", "--no-privileges"}, base...)
		}
		if mode == "restore" {
			return env, "psql", append([]string{"-q"}, base...)
		}
		return env, "psql", base
	default:
		if mode == "dump" {
			return nil, "sqlite3", []string{c.Name, ".dump"}
		}
		return nil, "sqlite3", []string{c.Name}
	}
}

var dbVia string

var dbCmd = &cobra.Command{
	Use:               "db [имя] [dump|restore <файл>]",
	Short:             "Клиент, дамп или восстановление базы по .env проекта",
	Long:              "С --via <алиас> база открывается через ssh-туннель к серверу: локальный порт подбирается, туннель снимается на выходе.",
	ValidArgsFunction: projectNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		name, action, file := "", "shell", ""
		for _, a := range args {
			switch {
			case a == "dump" || a == "restore":
				action = a
			case action == "restore" && file == "":
				file = a
			case name == "":
				name = a
			}
		}
		p, err := projectFor(name)
		if err != nil {
			return err
		}
		dir := cfg.ProjectDir(p)
		c, err := loadDB(p, dir)
		if err != nil {
			return err
		}
		if dbVia != "" {
			stop, err := viaTunnel(c, dbVia)
			if err != nil {
				return err
			}
			defer stop()
		}
		if c.Driver == "sqlite" {
			ui.Println(ui.Label(p.Name) + "  " + ui.Dim("sqlite "+c.Name))
		} else {
			ui.Println(ui.Label(p.Name) + "  " + ui.Dim(fmt.Sprintf("%s %s @ %s:%s", c.Driver, c.Name, c.Host, c.Port)))
		}
		switch action {
		case "shell":
			env, bin, cargs := c.clientArgs("shell")
			return runner.ExecEnv(env, bin, cargs...)
		case "dump":
			return dbDump(c)
		case "restore":
			return dbRestore(c, file)
		}
		return nil
	},
}

func dumpsDir() string {
	return filepath.Join(cfg.Dev, "data", "db-dumps")
}

func dbDump(c *dbConn) error {
	out := filepath.Join(dumpsDir(), filepath.Base(c.Name)+"-"+time.Now().Format("20060102-1504")+".sql.gz")
	env, bin, args := c.clientArgs("dump")
	if runner.DryRun {
		return runner.Stream(env, nil, nil, "| gzip > "+out, bin, args...)
	}
	if err := os.MkdirAll(dumpsDir(), 0o755); err != nil {
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
		// Недописанный дамп хуже отсутствующего: его легко принять за бэкап.
		_ = os.Remove(out)
		return ui.Errorf("дамп не снят: %v", err)
	}
	st, err := os.Stat(out)
	if err != nil {
		return err
	}
	ui.Println(ui.Label("дамп: ") + out + ui.Dim(fmt.Sprintf(" (%.1f MB)", float64(st.Size())/1e6)))
	return nil
}

func dbRestore(c *dbConn, file string) error {
	if file == "" {
		return ui.Errorf("укажи файл: tim db restore <файл.sql[.gz]>")
	}
	if _, err := os.Stat(file); err != nil {
		return ui.Errorf("файла нет: %s", file)
	}
	if !runner.Confirm(fmt.Sprintf("залить %s в %s %s, текущие данные будут перезаписаны?", filepath.Base(file), c.Driver, c.Name)) {
		return nil
	}
	env, bin, args := c.clientArgs("restore")
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
	if err := runner.Stream(env, in, os.Stdout, "", bin, args...); err != nil {
		return err
	}
	ui.Println(ui.Label("восстановлено из ") + filepath.Base(file))
	return nil
}

// viaTunnel поднимает ssh -L к базе на сервере и подменяет host:port на локальные.
func viaTunnel(c *dbConn, via string) (func(), error) {
	if c.Driver == "sqlite" {
		return nil, ui.Errorf("sqlite через туннель не открыть")
	}
	target, err := sshTarget(via)
	if err != nil {
		return nil, err
	}
	local, err := freePort()
	if err != nil {
		return nil, err
	}
	t := sshx.Tunnel{Name: "db-" + local, Host: target, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", local, c.Host, c.Port, target)}
	if ip := sshx.IP(target); ip != "" {
		ensureBypass(ip)
	}
	if err := t.Start("-L", "127.0.0.1:"+local+":"+c.Host+":"+c.Port); err != nil {
		return nil, ui.Errorf("туннель до %s не поднялся: %v", target, err)
	}
	ui.Println(ui.Dim("туннель " + t.Desc))
	c.Host, c.Port = "127.0.0.1", local
	return func() { t.Stop(); ui.Println(ui.Dim("туннель снят")) }, nil
}

func freePort() (string, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer func() { _ = l.Close() }()
	_, port, _ := net.SplitHostPort(l.Addr().String())
	return port, nil
}

func init() {
	dbCmd.Flags().StringVar(&dbVia, "via", "", "ssh-алиас или проект: открыть базу через туннель к серверу")
	root.AddCommand(dbCmd)
}
