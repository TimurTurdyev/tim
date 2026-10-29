package srvinfo

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsbl"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/runner"
)

// weakAlgos: то, что ssh-audit считает слабым и что OpenSSH давно не предлагает сам.
var weakAlgos = []string{"diffie-hellman-group1-sha1", "diffie-hellman-group14-sha1", "diffie-hellman-group-exchange-sha1",
	"ssh-dss", "ssh-rsa", "3des-cbc", "aes128-cbc", "aes192-cbc", "aes256-cbc", "blowfish-cbc", "cast128-cbc", "arcfour",
	"arcfour128", "arcfour256", "hmac-md5", "hmac-md5-96", "hmac-sha1-96", "hmac-md5-etm@openssh.com"}

// Outside: что видно снаружи, с этой машины, без входа и без root.
type Outside struct {
	HostKey   string         `json:"host_key,omitempty"`  // SHA256:... - сменится, будет новым
	Changed   bool           `json:"host_key_changed"`    // ssh сам сказал, что ключ хоста другой
	Methods   []string       `json:"methods,omitempty"`   // publickey, password, keyboard-interactive
	Weak      []string       `json:"weak,omitempty"`      // слабые алгоритмы, которые сервер предлагает
	Blacklist []dnsbl.Result `json:"blacklist,omitempty"` // адрес сервера в чёрных списках почты
	Ports     map[int]bool   `json:"ports,omitempty"`     // опубликованные Docker порты: отвечают ли снаружи
	Took      time.Duration  `json:"-"`
}

// ProbeSSH: рукопожатие ssh без входа - алгоритмы сервера, ключ хоста и какие способы входа он принимает.
// Без мультиплексора: через открытое соединение рукопожатия не видно.
func ProbeSSH(target string) Outside {
	out, _ := runner.CombinedWithin(20*time.Second, "ssh", "-vv", "-o", "BatchMode=yes", "-o", "ControlMaster=no",
		"-o", "ControlPath=none", "-o", "PreferredAuthentications=none", "-o", "ConnectTimeout=10", target, "true")
	return parseProbe(out)
}

func parseProbe(out string) Outside {
	var o Outside
	server := false
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		switch {
		case strings.Contains(l, "REMOTE HOST IDENTIFICATION HAS CHANGED"):
			o.Changed = true
		case strings.Contains(l, "peer server KEXINIT proposal"):
			server = true
		case strings.Contains(l, "first_kex_follows"):
			server = false
		case server:
			_, list, ok := strings.Cut(l, ": ")
			if !ok {
				continue
			}
			for _, a := range strings.Split(list, ",") {
				if slices.Contains(weakAlgos, a) && !slices.Contains(o.Weak, a) {
					o.Weak = append(o.Weak, a)
				}
			}
		case strings.HasPrefix(l, "debug1: Server host key:"):
			if f := strings.Fields(l); len(f) >= 6 {
				o.HostKey = f[5]
			}
		case strings.HasPrefix(l, "debug1: Authentications that can continue:"):
			o.Methods = strings.Split(strings.TrimSpace(strings.TrimPrefix(l, "debug1: Authentications that can continue:")), ",")
		}
	}
	return o
}

// outside: находки по проверкам снаружи.
func (f *findings) outside(r Report) {
	o := r.Outside
	if o == nil {
		return
	}
	if o.Changed {
		f.add("ключ хоста", level.Bad, "сменился", "ssh говорит, что ключ хоста другой: сервер переустановлен или соединение кто-то перехватывает",
			"если сервер не переустанавливал - не входи и выясни, кто посередине; иначе ssh-keygen -R <адрес>")
	}
	// Способы входа снаружи нужны, только когда sshd -T не прочитан: иначе это уже сказано по настройкам.
	if r.NoSudoSSHD && slices.ContainsFunc(o.Methods, func(m string) bool { return m == "password" || m == "keyboard-interactive" }) {
		f.add("ssh", level.Warn, "вход по паролю", "снаружи sshd принимает вход по паролю ("+strings.Join(o.Methods, ", ")+") - его перебирают",
			"вход по ключу: ssh-copy-id, затем PasswordAuthentication no и KbdInteractiveAuthentication no")
	}
	if len(o.Weak) > 0 {
		f.add("ssh", level.Warn, "слабые алгоритмы", "sshd предлагает устаревшие алгоритмы: "+strings.Join(o.Weak, ", "),
			"убрать их из Ciphers, MACs, KexAlgorithms и HostKeyAlgorithms в sshd_config; проверит ssh-audit")
	}
	var listed, unknown []string
	for _, b := range o.Blacklist {
		switch {
		case b.Listed:
			listed = append(listed, b.Zone)
		case b.Unknown != "":
			unknown = append(unknown, b.Zone+" "+b.Unknown)
		}
	}
	if len(listed) > 0 {
		f.add("чёрный список", level.Warn, strings.Join(listed, ", "), "адрес сервера в чёрных списках почты: "+strings.Join(listed, ", ")+
			" - письма с него будут отклонять", "узнать причину и снять: страница списка по адресу сервера; чаще всего - взломанная форма или ящик")
	} else if len(unknown) > 0 {
		f.add("чёрный список", level.Skip, "не всё проверено", strings.Join(unknown, "; "), "")
	}
	for _, p := range r.Watch.Docker {
		if open, ok := o.Ports[p.Port]; ok && open {
			f.add("порт", level.Bad, fmt.Sprintf("%d %s снаружи", p.Port, p.Name),
				fmt.Sprintf("%s:%d отвечает из интернета, хотя ufw включён - Docker открыл его мимо ufw", p.Name, p.Port),
				fmt.Sprintf("публикуй как 127.0.0.1:%d:%d или закрой в цепочке DOCKER-USER", p.Port, p.Port))
		}
	}
}
