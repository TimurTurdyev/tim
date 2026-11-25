package domaudit

import (
	"fmt"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/sweep"
)

const starZone = "example.test"

// starServer: зона на петле. names - явные имена и их адреса ("" - имя без адреса); под явным именем запись * не
// действует. star - адрес записи *, bare - запись * без адреса, silent - какие вопросы остаются без ответа.
type starServer struct {
	names  map[string]string
	star   string
	bare   bool
	silent func(name string) bool
}

func (z starServer) serve(t *testing.T) dnsPath {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if reply := z.reply(buf[:n]); reply != nil {
				_, _ = conn.WriteTo(reply, from)
			}
		}
	}()
	return dnsPath{opt: sweep.Options{Servers: []string{conn.LocalAddr().String()}}, zone: starZone, direct: true}
}

func (z starServer) reply(msg []byte) []byte {
	var p dnsmessage.Parser
	head, err := p.Start(msg)
	if err != nil {
		return nil
	}
	q, err := p.Question()
	name := strings.ToLower(strings.TrimSuffix(q.Name.String(), "."))
	if err != nil || z.silent != nil && z.silent(name) {
		return nil
	}
	ip, exists := z.lookup(name)
	out := dnsmessage.Header{ID: head.ID, Response: true, Authoritative: true}
	if !exists {
		out.RCode = dnsmessage.RCodeNameError
	}
	b := dnsmessage.NewBuilder(nil, out)
	_ = b.StartQuestions()
	_ = b.Question(q)
	_ = b.StartAnswers()
	if ip != "" {
		_ = b.AResource(dnsmessage.ResourceHeader{Name: q.Name, Class: dnsmessage.ClassINET, TTL: 60},
			dnsmessage.AResource{A: [4]byte(net.ParseIP(ip).To4())})
	}
	reply, err := b.Finish()
	if err != nil {
		return nil
	}
	return reply
}

func (z starServer) lookup(name string) (ip string, exists bool) {
	if ip, ok := z.names[name]; ok {
		return ip, true
	}
	for explicit := range z.names {
		if strings.HasSuffix(name, "."+explicit) {
			return "", false
		}
	}
	return z.star, z.star != "" || z.bare
}

func starNames(words ...string) []string {
	out := make([]string, len(words))
	for i, w := range words {
		out[i] = w + "." + starZone
	}
	return out
}

// Перебор в зоне с записью *: выдуманные имена не находки, заведённые явно - находки, даже с тем же адресом и в
// конце списка; когда о записи * судить нечем, проход помечается неполным, а не выдаёт всё за живое.
func TestAliveUnderStar(t *testing.T) {
	const star, other = "192.0.2.1", "192.0.2.9"
	var filler []string
	for i := range 12 {
		filler = append(filler, fmt.Sprintf("w%04d", i))
	}
	list := starNames(slices.Concat([]string{"mail", "real", "www.blog", "node"}, filler, []string{"www.w0001", "late"})...)
	zone := func(name string) string { return name + "." + starZone }
	cases := []struct {
		name              string
		zone              starServer
		live, firm, nodes []string
		starred           int
		cut               bool
	}{
		{name: "запись * под вершиной", zone: starServer{star: star, names: map[string]string{
			zone("mail"): star, zone("late"): star, zone("real"): other, zone("blog"): "", zone("www.blog"): other, zone("node"): ""}},
			live: starNames("real", "www.blog"), firm: starNames("mail", "late"), nodes: starNames("node"), starred: 15},
		{name: "записи * нет", zone: starServer{names: map[string]string{
			zone("mail"): star, zone("real"): other, zone("blog"): "", zone("www.blog"): other, zone("node"): ""}},
			live: starNames("mail", "real", "www.blog"), nodes: starNames("node")},
		{name: "запись * без адреса", zone: starServer{bare: true, names: map[string]string{zone("real"): other}},
			live: starNames("real")},
		{name: "зона молчит о несуществующем имени", zone: starServer{names: map[string]string{zone("mail"): star, zone("real"): other},
			silent: func(n string) bool { return strings.HasPrefix(n, "tim-nx-") }},
			firm: starNames("mail", "real"), starred: 2, cut: true},
		{name: "часть имён без ответа", zone: starServer{names: map[string]string{zone("mail"): star},
			silent: func(n string) bool { return strings.HasPrefix(n, "w000") }},
			live: starNames("mail"), cut: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := newCollector(starZone, Options{}).alive(c.zone.serve(t), list, time.Second)
			slices.Sort(got.live)
			slices.Sort(got.firm)
			want := found{live: slices.Sorted(slices.Values(c.live)), firm: slices.Sorted(slices.Values(c.firm)), nodes: c.nodes,
				starred: c.starred, cut: c.cut}
			if !slices.Equal(got.live, want.live) || !slices.Equal(got.firm, want.firm) || !slices.Equal(got.nodes, want.nodes) ||
				got.starred != want.starred || got.cut != want.cut {
				t.Errorf("живые %v, явные %v, без адреса %v, под * %d, неполно %v\n ждали %v, %v, %v, %d, %v",
					got.live, got.firm, got.nodes, got.starred, got.cut, want.live, want.firm, want.nodes, want.starred, want.cut)
			}
		})
	}
}
