// Package vpnhelp: разбор целей обхода старого демона vpn-bypass-routes, чтобы перенести их в tim.
package vpnhelp

import (
	"strings"
)

// Пути, куда ставит себя старый демон.
const (
	Label  = "com.vpnhelp.bypass-routes"
	Plist  = "/Library/LaunchDaemons/" + Label + ".plist"
	Script = "/usr/local/sbin/vpn-bypass-routes.sh"
	List   = "/usr/local/etc/vpn-bypass.list"
)

// Item: цель и пометка из комментария.
type Item struct {
	Addr  string
	Label string
}

// ParseList: строки "CIDR или домен  # комментарий"; пустые и чистые комментарии пропускаются.
func ParseList(text string) []Item {
	var out []Item
	for _, line := range strings.Split(text, "\n") {
		body, comment, _ := strings.Cut(line, "#")
		f := strings.Fields(body)
		if len(f) == 0 {
			continue
		}
		out = append(out, Item{Addr: f[0], Label: strings.TrimSpace(comment)})
	}
	return out
}

// ParseScript: сети из массива NETS=( ... ) скрипта. Комментарий над группой становится пометкой.
func ParseScript(text string) []Item {
	var out []Item
	inside := false
	label := ""
	for _, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case !inside && strings.HasPrefix(t, "NETS=("):
			inside = true
			t = strings.TrimSpace(strings.TrimPrefix(t, "NETS=("))
			if t == "" {
				continue
			}
		case !inside:
			continue
		}
		if strings.HasPrefix(t, ")") {
			break
		}
		if c, ok := strings.CutPrefix(t, "#"); ok {
			label = strings.TrimSpace(c)
			continue
		}
		body, comment, _ := strings.Cut(t, "#")
		for _, f := range strings.Fields(strings.TrimSuffix(body, ")")) {
			l := strings.TrimSpace(comment)
			if l == "" {
				l = label
			}
			out = append(out, Item{Addr: strings.Trim(f, `"'`), Label: l})
		}
		if strings.HasSuffix(strings.TrimSpace(body), ")") {
			break
		}
	}
	return out
}
