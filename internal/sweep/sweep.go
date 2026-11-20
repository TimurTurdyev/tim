// Package sweep: массовая проверка имён через DNS - у серверов зоны с подстройкой частоты или по DoH.
package sweep

import (
	"context"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	// stallLimit: столько без единого ответа по существу - перебор кончается и без срока
	stallLimit    = 10 * time.Second
	progressEvery = 100 * time.Millisecond
	// stopMargin: запас к сроку ответа, с которым новые имена перестают выдаваться перед концом срока
	stopMargin = 50 * time.Millisecond
)

// DoHServers: резолверы DNS поверх HTTPS, второй - запасной.
var DoHServers = []string{"https://dns.google/dns-query", "https://cloudflare-dns.com/dns-query"}

// Kind: что ответ говорит об имени. None и Fail - имя не проверено, а не "имени нет".
type Kind uint8

const (
	None   Kind = iota // ответа нет
	Addr               // есть записи запрошенного типа
	CNAME              // имя - псевдоним, цель может не существовать
	NoData             // имя есть, записей этого типа нет
	NX                 // имени нет
	Refer              // имя в дочерней зоне: спрашивать её серверы
	Fail               // сервер отказал
)

// Answer: ответ на одно имя. Target - цель CNAME или значение PTR, Zone - вершина дочерней зоны при отсылке.
type Answer struct {
	Name   string
	Kind   Kind
	IPs    []string
	Target string
	Zone   string
}

type Options struct {
	Servers   []string // "адрес:порт" серверов зоны, при Recursive - резолверов
	Recursive bool     // просить рекурсию: резолверы вместо серверов зоны
	DoH       []string // адреса DoH; не пусто - идём по HTTPS, Servers не нужны
	Control   func(network, address string, c syscall.RawConn) error
	Type      dnsmessage.Type // 0 - A
	Budget    time.Duration   // 0 - без срока
	Progress  func(done, total int)
}

// Result: Answers[i] - ответ на names[i]; длина Answers - сколько имён успели взять за срок.
type Result struct {
	Answers    []Answer
	Unanswered int
	// Limited: сервер ограничивал частоту - приходили обрезанные ответы
	Limited bool
	Elapsed time.Duration
}

// Run проверяет имена по порядку и останавливается по сроку: взятые имена идут подряд, без пропусков.
func Run(ctx context.Context, names []string, opt Options) Result {
	if opt.Type == 0 {
		opt.Type = dnsmessage.TypeA
	}
	start := time.Now()
	var res Result
	switch {
	case len(names) == 0:
	case len(opt.DoH) > 0:
		res = overHTTPS(ctx, names, opt, start)
	default:
		res = overUDP(ctx, names, opt, start)
	}
	res.Elapsed = time.Since(start)
	return res
}

func definite(k Kind) bool { return k != None && k != Fail }

// horizon: сколько от начала до срока - по Budget и по сроку контекста; 0 - срока нет.
func horizon(ctx context.Context, start time.Time, budget time.Duration) time.Duration {
	at, ok := ctx.Deadline()
	if !ok {
		return budget
	}
	left := max(at.Sub(start), time.Nanosecond)
	if budget == 0 || left < budget {
		return left
	}
	return budget
}

// probeName: общий родитель имён - о нём спрашивают, когда нужен любой ответ сервера.
func probeName(names []string) string {
	root := parent(names[0])
	for _, n := range names {
		for root != "" && !within(n, root) {
			root = parent(root)
		}
	}
	if !strings.Contains(root, ".") {
		return names[0]
	}
	return root
}

func parent(name string) string {
	_, rest, _ := strings.Cut(strings.TrimSuffix(name, "."), ".")
	return rest
}

// within: имя name - это zone или лежит под ней.
func within(name, zone string) bool {
	name, zone = strings.TrimSuffix(name, "."), strings.TrimSuffix(zone, ".")
	cut := len(name) - len(zone)
	if cut < 0 || !strings.EqualFold(name[cut:], zone) {
		return false
	}
	return cut == 0 || name[cut-1] == '.'
}

func sameName(a, b string) bool {
	return strings.EqualFold(strings.TrimSuffix(a, "."), strings.TrimSuffix(b, "."))
}
