package servers

import (
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/app"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
)

// insideLimit: алиас с именем вместо адреса ждёт системный DNS до 3 с, поэтому адреса узнаём пачкой.
const insideLimit = 16

// hostName: имя хоста с точкой; подчёркивание в метке допускают и nginx, и DNS.
var hostName = regexp.MustCompile(`^[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?(\.[a-z0-9_]([a-z0-9_-]{0,61}[a-z0-9_])?)+$`)

// Inside: что сервер сам знает о своих сайтах.
type Inside struct {
	Alias, IP   string
	Names, Wild []string
	At          time.Time
}

// InsideNames: серверы из ~/.ssh/config с адресом из ips и имена их сайтов из сохранённого обхода, без ssh.
func InsideNames(env *app.Env, ips []string) []Inside {
	if len(ips) == 0 {
		return nil
	}
	targets := Targets(env, nil)
	addrs := par.Map(targets, insideLimit, func(s Server) string { return sshx.IP(s.Alias) })
	var out []Inside
	for i, t := range targets {
		if addrs[i] == "" || !slices.Contains(ips, addrs[i]) {
			continue
		}
		e, at, ok := savedExplore(t.Alias)
		if !ok {
			continue
		}
		state, _, _ := saved(t.Alias)
		names, wild := siteNames(e.Sites, state.Report.Certs)
		out = append(out, Inside{Alias: t.Alias, IP: addrs[i], Names: names, Wild: wild, At: at})
	}
	return out
}

// siteNames: имена хостов и родители масок из server_name сайтов и папок сертификатов.
func siteNames(sites []srvinfo.Site, certs []srvinfo.Cert) (names, wild []string) {
	var raw []string
	for _, s := range sites {
		raw = append(raw, s.Name)
		raw = append(raw, s.Aliases...)
	}
	for _, c := range certs {
		raw = append(raw, certHost(c.Name))
	}
	for _, r := range raw {
		n := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(r)), ".")
		parent, mask := strings.CutPrefix(n, "*.")
		bare, dotted := strings.CutPrefix(n, ".")
		switch {
		case mask && plainHost(parent):
			wild = append(wild, parent)
		// у nginx ".example.com" - и само имя, и всё под ним
		case dotted && plainHost(bare):
			names, wild = append(names, bare), append(wild, bare)
		case plainHost(n):
			names = append(names, n)
		}
	}
	slices.Sort(names)
	slices.Sort(wild)
	return slices.Compact(names), slices.Compact(wild)
}

// certHost: certbot называет папку первым именем сертификата, а повторной дописывает -0001.
func certHost(dir string) string {
	i := strings.LastIndexByte(dir, '-')
	if i < 0 || len(dir)-i != 5 || strings.Trim(dir[i+1:], "0123456789") != "" {
		return dir
	}
	return dir[:i]
}

// plainHost: обычное имя хоста - не маска, не регулярное выражение, не адрес и не localhost.
func plainHost(name string) bool {
	if len(name) > 253 || !hostName.MatchString(name) {
		return false
	}
	if name == "localhost.localdomain" || strings.HasSuffix(name, ".localhost") {
		return false
	}
	// последняя метка из одних цифр бывает только у адреса
	last := name[strings.LastIndexByte(name, '.')+1:]
	return strings.Trim(last, "0123456789") != ""
}
