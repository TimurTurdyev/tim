package servers

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/dbx"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/sshx"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// database: клиент, дамп или восстановление базы проекта; via - через ssh-туннель к серверу.
func database(env *app.Env, args []string, via string) error {
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
	p, err := env.Project(name)
	if err != nil {
		return err
	}
	c, err := dbx.Load(env.Cfg.ProjectDir(p))
	if err != nil {
		return ui.Errorf("%s: %v", p.Name, err)
	}
	if via != "" {
		stop, err := viaTunnel(env, c, via)
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
	case "dump":
		return dump(c, filepath.Join(env.Cfg.Dev, "data", "db-dumps"))
	case "restore":
		return restore(c, file)
	}
	vars, bin, cargs := c.Client("shell")
	return runner.ExecEnv(vars, bin, cargs...)
}

func dump(c *dbx.Conn, dir string) error {
	out := filepath.Join(dir, filepath.Base(c.Name)+"-"+time.Now().Format("20060102-1504")+".sql.gz")
	if err := dbx.Dump(c, out); err != nil || runner.DryRun {
		return err
	}
	st, err := os.Stat(out)
	if err != nil {
		return err
	}
	ui.Println(ui.Label("дамп: ") + out + ui.Dim(fmt.Sprintf(" (%.1f MB)", float64(st.Size())/1e6)))
	return nil
}

func restore(c *dbx.Conn, file string) error {
	if file == "" {
		return ui.Errorf("укажи файл: tim db restore <файл.sql[.gz]>")
	}
	if _, err := os.Stat(file); err != nil {
		return ui.Errorf("файла нет: %s", file)
	}
	if !runner.Confirm(fmt.Sprintf("залить %s в %s %s, текущие данные будут перезаписаны?", filepath.Base(file), c.Driver, c.Name)) {
		return nil
	}
	if err := dbx.Restore(c, file); err != nil || runner.DryRun {
		return err
	}
	ui.Println(ui.Label("восстановлено из ") + filepath.Base(file))
	return nil
}

// viaTunnel поднимает ssh -L к базе на сервере и подменяет host:port на локальные.
func viaTunnel(env *app.Env, c *dbx.Conn, via string) (func(), error) {
	if c.Driver == "sqlite" {
		return nil, ui.Errorf("sqlite через туннель не открыть")
	}
	target, err := sshTarget(env.Cfg, via)
	if err != nil {
		return nil, err
	}
	local, err := netinfo.FreePort()
	if err != nil {
		return nil, err
	}
	t := sshx.Tunnel{Name: "db-" + local, Host: target, Desc: fmt.Sprintf("127.0.0.1:%s -> %s:%s на стороне %s", local, c.Host, c.Port, target)}
	ensureBypass(env.Cfg, target)
	if err := t.Start("-L", "127.0.0.1:"+local+":"+c.Host+":"+c.Port); err != nil {
		return nil, ui.Errorf("туннель до %s не поднялся: %v", target, err)
	}
	ui.Println(ui.Dim("туннель " + t.Desc))
	c.Host, c.Port = "127.0.0.1", local
	return func() { t.Stop(); ui.Println(ui.Dim("туннель снят")) }, nil
}
