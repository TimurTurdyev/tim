package audit

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsq"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/netinfo"
	"github.com/TimurTurdyev/tim/internal/par"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const zoneTimeout = 5 * time.Second

// zoneServer: сервер имён и что он ответил на просьбу отдать зону.
type zoneServer struct {
	name string
	got  dnsq.Transfer
	err  error
}

// zoneTransfer: отдаёт ли кто-то из серверов имён всю зону любому, кто попросит.
func zoneTransfer(s *site) []Finding {
	if isIP(s.host) || !strings.Contains(s.host, ".") {
		return nil
	}
	if dnsq.Hijacked() {
		return []Finding{zoneUnknown()}
	}
	zone := inspect.Registrable(s.host)
	return ZoneFindings(zone, par.Map(zoneServers(zone), par.Limit, func(name string) zoneServer {
		got, err := askZone(name, zone)
		return zoneServer{name, got, err}
	}))
}

// ZoneFindings: открытая передача зоны по ответам её серверов имён.
func ZoneFindings(zone string, servers []zoneServer) []Finding {
	var out []Finding
	foreign := 0
	for _, s := range servers {
		if errors.Is(s.err, dnsq.ErrNotZone) {
			foreign++
		}
		if s.err != nil {
			continue
		}
		text := fmt.Sprintf("%s отдаёт всю зону любому: записей %d", s.name, s.got.Records)
		if s.got.Cut {
			text = fmt.Sprintf("%s отдаёт всю зону любому: записей больше %d", s.name, s.got.Records)
		}
		out = append(out, Finding{Area: "dns", Level: level.Bad, Text: text,
			Why:       "виден весь список имён, включая служебные: " + ui.Plain(strings.Join(s.got.Names, ", ")),
			Fix:       "в настройках сервера имён разрешить передачу только вторичным: allow-transfer { адреса; } в BIND",
			Intrusive: true})
	}
	if len(out) == 0 && foreign > 0 {
		return []Finding{zoneUnknown()}
	}
	return out
}

func zoneUnknown() Finding {
	return Finding{Area: "dns", Level: level.Info, Text: "передача зоны не проверена: на порту 53 отвечает не сервер зоны, а кто-то на пути",
		Why: "так ведут себя VPN и перехват DNS: их отказ ничего не говорит о сервере", Fix: "проверить без VPN или с сервера в другой сети", Intrusive: true}
}

func zoneServers(zone string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), zoneTimeout)
	defer cancel()
	records, _ := dnsq.Resolver().LookupNS(ctx, zone)
	var out []string
	for _, r := range records {
		out = append(out, strings.TrimSuffix(r.Host, "."))
	}
	return out
}

// askZone: просьба отдать зону первому публичному адресу сервера имён.
func askZone(server, zone string) (dnsq.Transfer, error) {
	ctx, cancel := context.WithTimeout(context.Background(), zoneTimeout)
	defer cancel()
	ips, err := dnsq.Resolver().LookupIP(ctx, "ip4", server)
	if err != nil {
		return dnsq.Transfer{}, err
	}
	for _, ip := range ips {
		if !netinfo.IsPublic(ip.String()) {
			continue
		}
		conn, err := webnet.Dial("tcp", net.JoinHostPort(ip.String(), "53"), zoneTimeout)
		if err != nil {
			return dnsq.Transfer{}, err
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(zoneTimeout))
		return dnsq.AskZone(conn, zone)
	}
	return dnsq.Transfer{}, errors.New("у " + server + " нет публичного адреса")
}
