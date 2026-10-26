package srvinfo

import (
	"slices"
	"testing"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
)

// Образец - разделы скрипта с сервера, где есть всё сразу: свой и подкинутый ключ, майнер из /tmp,
// служба со старым бинарником, Docker мимо ufw.
const watchSample = `@@firewall
Status: active
@@keys
key root 256 SHA256:aaa timurt@mac (ED25519)
key root 3072 SHA256:bbb deploy@unknown (RSA)
read root
read deploy
unread www
@@suspect
exe 4242 kdevtmpfsi /tmp/.x/kdevtmpfsi (deleted)
exe 700 sshd /usr/sbin/sshd (deleted)
exe 900 app /home/deploy/app (deleted)
preload /usr/lib/libprocesshider.so
@@stale
dbus-daemon
@@docker
redis|0.0.0.0:6379->6379/tcp, :::6379->6379/tcp
web|127.0.0.1:8080->80/tcp
@@end
`

func TestParseWatch(t *testing.T) {
	r := Parse(watchSample)
	w := r.Watch
	if len(w.Keys["root"]) != 2 || w.Keys["root"][1].Comment != "deploy@unknown" || w.Keys["deploy"] == nil {
		t.Fatalf("ключи: %+v", w.Keys)
	}
	if _, ok := w.Keys["www"]; ok {
		t.Fatal("непрочитанный пользователь попал в ключи: его нельзя сравнивать")
	}
	if !slices.Equal(w.Stale, []string{"dbus-daemon", "sshd"}) || !w.StaleRead {
		t.Fatalf("старые библиотеки: %v", w.Stale)
	}
	if len(w.Docker) != 2 || w.Docker[0].Addr != "0.0.0.0" || w.Docker[1].Addr != "127.0.0.1" {
		t.Fatalf("docker: %+v", w.Docker)
	}
	bad := map[string]bool{}
	for _, c := range Evaluate(r, time.Now()) {
		if c.Level == level.Bad {
			bad[c.Name] = true
		}
	}
	if !bad["процесс"] || !bad["ld.so.preload"] {
		t.Fatalf("признаки взлома не найдены: %v", bad)
	}
}

func TestParseProbe(t *testing.T) {
	o := parseProbe(`debug2: peer server KEXINIT proposal
debug2: KEX algorithms: curve25519-sha256,diffie-hellman-group14-sha1
debug2: host key algorithms: rsa-sha2-512,ssh-rsa,ssh-ed25519
debug2: ciphers ctos: aes128-ctr,aes256-cbc
debug2: first_kex_follows 0
debug2: ciphers ctos: aes256-cbc
debug1: Server host key: ssh-ed25519 SHA256:Xqua
debug1: Authentications that can continue: publickey,password`)
	if o.HostKey != "SHA256:Xqua" || !slices.Equal(o.Methods, []string{"publickey", "password"}) ||
		!slices.Equal(o.Weak, []string{"diffie-hellman-group14-sha1", "ssh-rsa", "aes256-cbc"}) {
		t.Fatalf("%+v", o)
	}
}
