package cmd

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Корневой помощник: один пароль sudo на запуск tim. Первый rootRun поднимает "sudo tim root-helper",
// sudo спрашивает пароль, дальше команды уходят помощнику по трубе - сколько бы маршрутов ни
// заводилось. Под root (страж маршрутов в launchd) помощник не нужен, всё делается на месте.

type rootReq struct {
	Cmd    []string   `json:"cmd,omitempty"`
	Stdin  string     `json:"stdin,omitempty"`
	Launch *launchReq `json:"launch,omitempty"`
}

// launchReq: запустить программу от имени пользователя, но под другой группой (мимо VPN через pf).
type launchReq struct {
	Path   string   `json:"path"`
	Args   []string `json:"args,omitempty"`
	Env    []string `json:"env,omitempty"`
	Dir    string   `json:"dir,omitempty"`
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	Groups []int    `json:"groups,omitempty"`
	Wait   bool     `json:"wait,omitempty"` // дождаться и вернуть вывод, а не отпустить в фон
}

type rootResp struct {
	Out string `json:"out"`
	Err string `json:"err,omitempty"`
	PID int    `json:"pid,omitempty"`
}

// rootAllowed: что помощник вообще запускает. Это не защита от злоумышленника (sudo и так у
// пользователя), а страховка от ошибки в коде.
var rootAllowed = map[string]bool{"route": true, "ip": true, "pfctl": true, "dscl": true, "dseditgroup": true}

var rootHelperCmd = &cobra.Command{
	Use:    "root-helper",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   func(cmd *cobra.Command, args []string) error { return serveRoot(os.Stdin, os.Stdout) },
}

func serveRoot(in io.Reader, out io.Writer) error {
	if os.Geteuid() != 0 {
		return ui.Errorf("root-helper запускает sudo tim, руками он не нужен")
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req rootReq
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(rootResp{Err: err.Error()})
			continue
		}
		_ = enc.Encode(handleRoot(req))
	}
	return nil
}

func handleRoot(req rootReq) rootResp {
	switch {
	case req.Launch != nil:
		return launchAs(*req.Launch)
	case len(req.Cmd) > 0 && rootAllowed[req.Cmd[0]]:
		c := exec.Command(req.Cmd[0], req.Cmd[1:]...)
		if req.Stdin != "" {
			c.Stdin = strings.NewReader(req.Stdin)
		}
		b, err := c.CombinedOutput()
		r := rootResp{Out: strings.TrimSpace(string(b))}
		if err != nil {
			r.Err = err.Error()
			if r.Out != "" {
				r.Err = r.Out
			}
		}
		return r
	}
	return rootResp{Err: "помощник такого не делает"}
}

func launchAs(l launchReq) rootResp {
	c := exec.Command(l.Path, l.Args...)
	c.Env, c.Dir = l.Env, l.Dir
	groups := make([]uint32, len(l.Groups))
	for i, g := range l.Groups {
		groups[i] = uint32(g)
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: &syscall.Credential{Uid: uint32(l.UID), Gid: uint32(l.GID), Groups: groups}}
	if l.Wait {
		b, err := c.CombinedOutput()
		r := rootResp{Out: strings.TrimSpace(string(b))}
		if err != nil {
			r.Err = err.Error()
		}
		return r
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return rootResp{Err: err.Error()}
	}
	c.Stdin, c.Stdout, c.Stderr = devnull, devnull, devnull
	if err := c.Start(); err != nil {
		return rootResp{Err: err.Error()}
	}
	// Программа живёт сама (своя сессия), помощнику её ждать незачем.
	go func() { _ = c.Wait() }()
	return rootResp{PID: c.Process.Pid}
}

// ---------- клиент ----------

type rootLink struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Scanner
}

var rootHelper *rootLink

func rootStart() (*rootLink, error) {
	if rootHelper != nil {
		return rootHelper, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	c := exec.Command("sudo", "-p", "пароль администратора (один раз на запуск tim): ", exe, "root-helper")
	c.Stderr = os.Stderr
	in, err := c.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := c.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := c.Start(); err != nil {
		return nil, ui.Errorf("sudo: %v", err)
	}
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	rootHelper = &rootLink{cmd: c, in: in, out: sc}
	return rootHelper, nil
}

func rootCall(req rootReq) (rootResp, error) {
	if os.Geteuid() == 0 {
		return handleRoot(req), nil
	}
	if runner.DryRun {
		switch {
		case req.Launch != nil:
			ui.Println(ui.Dim("$ sudo -u #" + strconv.Itoa(req.Launch.UID) + " -g #" + strconv.Itoa(req.Launch.GID) + " " + shellq.Join(append([]string{req.Launch.Path}, req.Launch.Args...))))
		default:
			ui.Println(ui.Dim("$ sudo " + shellq.Join(req.Cmd)))
		}
		return rootResp{}, nil
	}
	l, err := rootStart()
	if err != nil {
		return rootResp{}, err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return rootResp{}, err
	}
	if _, err := l.in.Write(append(data, '\n')); err != nil {
		rootHelper = nil
		return rootResp{}, ui.Errorf("помощник не отвечает: %v", err)
	}
	if !l.out.Scan() {
		rootHelper = nil
		return rootResp{}, ui.Errorf("прав администратора не получил: sudo отказал или помощник упал")
	}
	var resp rootResp
	if err := json.Unmarshal(l.out.Bytes(), &resp); err != nil {
		return rootResp{}, err
	}
	return resp, nil
}

// rootRun выполняет команду под root и возвращает её вывод; ошибка - с текстом от самой команды.
func rootRun(args ...string) (string, error) {
	return rootRunStdin("", args...)
}

func rootRunStdin(stdin string, args ...string) (string, error) {
	resp, err := rootCall(rootReq{Cmd: args, Stdin: stdin})
	if err != nil {
		return "", err
	}
	if resp.Err != "" {
		return resp.Out, errors.New(resp.Err)
	}
	return resp.Out, nil
}

func init() {
	root.AddCommand(rootHelperCmd)
}
