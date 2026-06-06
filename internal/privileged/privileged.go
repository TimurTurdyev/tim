package privileged

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Корневой помощник: один пароль sudo на запуск tim. Первый Run поднимает "sudo tim root-helper",
// sudo спрашивает пароль, дальше команды уходят помощнику по трубе - сколько бы маршрутов ни
// заводилось. Под root (страж маршрутов в launchd) помощник не нужен, всё делается на месте.

// HelperArg: скрытая команда tim, под которой работает помощник.
const HelperArg = "root-helper"

// allowed: что помощник вообще запускает. Это не защита от злоумышленника (sudo и так у
// пользователя), а страховка от ошибки в коде.
var allowed = map[string]bool{"route": true, "ip": true, "pfctl": true, "dscl": true, "dseditgroup": true, "kill": true, "sysctl": true,
	"iptables": true, "iptables-nft": true, "iptables-legacy": true, "groupadd": true}

var (
	link   *conn
	failed error // sudo уже отказал: второй раз за запуск не спрашиваем
)

type Request struct {
	Cmd    []string   `json:"cmd,omitempty"`
	Stdin  string     `json:"stdin,omitempty"`
	Launch *LaunchReq `json:"launch,omitempty"`
}

// LaunchReq: запустить программу от имени пользователя, но под другой группой (мимо VPN через pf).
type LaunchReq struct {
	Path   string   `json:"path"`
	Args   []string `json:"args,omitempty"`
	Env    []string `json:"env,omitempty"`
	Dir    string   `json:"dir,omitempty"`
	UID    int      `json:"uid"`
	GID    int      `json:"gid"`
	Groups []int    `json:"groups,omitempty"`
	Wait   bool     `json:"wait,omitempty"` // дождаться и вернуть вывод, а не отпустить в фон
}

type Response struct {
	Out string `json:"out"`
	Err string `json:"err,omitempty"`
	PID int    `json:"pid,omitempty"`
}

// Serve: сторона root - читает запросы построчно и отвечает на каждый.
func Serve(in io.Reader, out io.Writer) error {
	if os.Geteuid() != 0 {
		return ui.Errorf("root-helper запускает sudo tim, руками он не нужен")
	}
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	enc := json.NewEncoder(out)
	for sc.Scan() {
		var req Request
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			_ = enc.Encode(Response{Err: err.Error()})
			continue
		}
		_ = enc.Encode(handle(req))
	}
	return nil
}

func handle(req Request) Response {
	switch {
	case req.Launch != nil:
		return launchAs(*req.Launch)
	case len(req.Cmd) > 0 && allowed[filepath.Base(req.Cmd[0])]:
		c := exec.Command(req.Cmd[0], req.Cmd[1:]...)
		if req.Stdin != "" {
			c.Stdin = strings.NewReader(req.Stdin)
		}
		b, err := c.CombinedOutput()
		r := Response{Out: strings.TrimSpace(string(b))}
		if err != nil {
			r.Err = err.Error()
			if r.Out != "" {
				r.Err = r.Out
			}
		}
		return r
	}
	return Response{Err: "помощник такого не делает"}
}

func launchAs(l LaunchReq) Response {
	c := exec.Command(l.Path, l.Args...)
	c.Env, c.Dir = l.Env, l.Dir
	groups := make([]uint32, len(l.Groups))
	for i, g := range l.Groups {
		groups[i] = uint32(g)
	}
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Credential: &syscall.Credential{Uid: uint32(l.UID), Gid: uint32(l.GID), Groups: groups}}
	if l.Wait {
		b, err := c.CombinedOutput()
		r := Response{Out: strings.TrimSpace(string(b))}
		if err != nil {
			r.Err = err.Error()
		}
		return r
	}
	devnull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return Response{Err: err.Error()}
	}
	c.Stdin, c.Stdout, c.Stderr = devnull, devnull, devnull
	if err := c.Start(); err != nil {
		return Response{Err: err.Error()}
	}
	// Программа живёт сама (своя сессия), помощнику её ждать незачем.
	go func() { _ = c.Wait() }()
	return Response{PID: c.Process.Pid}
}

// ---------- клиент ----------

type conn struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Scanner
}

func start() (*conn, error) {
	if link != nil {
		return link, nil
	}
	if failed != nil {
		return nil, failed
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	c := exec.Command("sudo", "-p", "пароль администратора (один раз на запуск tim): ", exe, HelperArg)
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
	link = &conn{cmd: c, in: in, out: sc}
	return link, nil
}

func Call(req Request) (Response, error) {
	// Сначала dry-run: под root помощник не нужен, но и делать ничего нельзя.
	if runner.DryRun {
		switch {
		case req.Launch != nil:
			ui.Println(ui.Dim("$ sudo -u #" + strconv.Itoa(req.Launch.UID) + " -g #" + strconv.Itoa(req.Launch.GID) + " " + shellq.Join(append([]string{req.Launch.Path}, req.Launch.Args...))))
		default:
			ui.Println(ui.Dim("$ sudo " + shellq.Join(req.Cmd)))
		}
		return Response{}, nil
	}
	if os.Geteuid() == 0 {
		return handle(req), nil
	}
	l, err := start()
	if err != nil {
		return Response{}, err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return Response{}, err
	}
	if _, err := l.in.Write(append(data, '\n')); err != nil {
		link = nil
		return Response{}, ui.Errorf("помощник не отвечает: %v", err)
	}
	if !l.out.Scan() {
		link = nil
		failed = ui.Errorf("прав администратора не получил: sudo отказал или помощник упал; нужен обычный терминал, где sudo может спросить пароль")
		return Response{}, failed
	}
	var resp Response
	if err := json.Unmarshal(l.out.Bytes(), &resp); err != nil {
		return Response{}, err
	}
	return resp, nil
}

// Run выполняет команду под root и возвращает её вывод; ошибка - с текстом от самой команды.
func Run(args ...string) (string, error) {
	return RunStdin("", args...)
}

// Launch запускает программу под нужными uid и gid; Wait - дождаться и вернуть вывод.
func Launch(l LaunchReq) (Response, error) {
	return Call(Request{Launch: &l})
}

func RunStdin(stdin string, args ...string) (string, error) {
	resp, err := Call(Request{Cmd: args, Stdin: stdin})
	if err != nil {
		return "", err
	}
	if resp.Err != "" {
		return resp.Out, errors.New(resp.Err)
	}
	return resp.Out, nil
}
