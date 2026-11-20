package dnsq

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/dns/dnsmessage"

	"github.com/TimurTurdyev/tim/internal/bindif"
	"github.com/TimurTurdyev/tim/internal/webnet"
)

const (
	// probe: 192.0.2.1 - адрес для документации, DNS-сервера там нет и быть не может.
	probe = "192.0.2.1:53"
	// tcpWait: срок на соединение и ответ по TCP.
	tcpWait = 4 * time.Second
)

var (
	// tries: сроки попыток на одном сокете; запоздавший ответ на прежнюю попытку тоже годится.
	tries = []time.Duration{time.Second, time.Second, time.Second}
	// bindTries: резолвер отвечает на вопрос о корне из кэша, долго ждать его при проверке интерфейса незачем.
	bindTries  = []time.Duration{400 * time.Millisecond, 600 * time.Millisecond, time.Second}
	probeTries = []time.Duration{500 * time.Millisecond}
	// boundProbeTries: на прямом пути первый пакет нового потока теряется чаще, поэтому вопрос уходит дважды.
	boundProbeTries = []time.Duration{300 * time.Millisecond, 400 * time.Millisecond}

	bound         atomic.Pointer[binding]
	plainHijacked = sync.OnceValue(func() bool { return intercepted(open(nil), probeTries) })

	errHijacked    = errors.New("порт 53 перехвачен: до сервера зоны не достать, спросить можно только резолвер")
	errNoResolvers = errors.New("нет резолверов")
)

// binding: интерфейс, с которого запросы идут мимо маршрута по умолчанию.
type binding struct {
	iface    string
	control  func(network, address string, c syscall.RawConn) error
	resolver *net.Resolver
	turn     atomic.Uint32
}

// send: вопрос публичным резолверам по кругу; адрес из resolv.conf не годится - тот сервер внутри туннеля.
func (b *binding) send(ctx context.Context, msg []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(Resolvers) == 0 {
		return nil, errNoResolvers
	}
	server := Resolvers[int(b.turn.Add(1)-1)%len(Resolvers)]
	return ask(open(b.control), server, msg, fitting(ctx, tries))
}

// dialing: чем открыть сокет на срок.
type dialing func(limit time.Duration) *net.Dialer

// relay: соединение для резолвера Go, запрос уходит через send; оно не PacketConn, и разговор идёт как по tcp.
type relay struct {
	ctx  context.Context
	send func(ctx context.Context, msg []byte) ([]byte, error)
	out  bytes.Buffer
}

func (c *relay) Write(b []byte) (int, error) {
	if len(b) < 2 {
		return 0, errors.New("dnsq: короткий запрос")
	}
	resp, err := c.send(c.ctx, b[2:])
	if err != nil {
		return 0, err
	}
	c.out.Reset()
	_ = binary.Write(&c.out, binary.BigEndian, uint16(len(resp)))
	c.out.Write(resp)
	return len(b), nil
}

func (c *relay) Read(b []byte) (int, error) { return c.out.Read(b) }

func (c *relay) Close() error                     { return nil }
func (c *relay) LocalAddr() net.Addr              { return relayAddr{} }
func (c *relay) RemoteAddr() net.Addr             { return relayAddr{} }
func (c *relay) SetDeadline(time.Time) error      { return nil }
func (c *relay) SetReadDeadline(time.Time) error  { return nil }
func (c *relay) SetWriteDeadline(time.Time) error { return nil }

type relayAddr struct{}

func (relayAddr) Network() string { return "dnsq" }
func (relayAddr) String() string  { return "dnsq" }

// Bind: дальше запросы пакета идут с интерфейса iface мимо маршрута по умолчанию; при ошибке путь остаётся прежним.
func Bind(iface string) error {
	if b := bound.Load(); b != nil && b.iface == iface {
		return nil
	}
	control, err := bindif.Control(iface)
	if err != nil {
		return err
	}
	via := open(control)
	if err := reachable(via); err != nil {
		return fmt.Errorf("с интерфейса %s публичный резолвер не отвечает: %w", iface, err)
	}
	if intercepted(via, boundProbeTries) {
		return fmt.Errorf("порт 53 перехвачен и на интерфейсе %s", iface)
	}
	b := &binding{iface: iface, control: control}
	b.resolver = relayed(b.send)
	bound.Store(b)
	return nil
}

// Bound: интерфейс привязки; пусто - запросы идут обычным путём.
func Bound() string {
	if b := bound.Load(); b != nil {
		return b.iface
	}
	return ""
}

// Control: привязка для чужих сокетов, чтобы они шли тем же путём, что запросы пакета; nil - обычный путь.
func Control() func(network, address string, c syscall.RawConn) error {
	if b := bound.Load(); b != nil {
		return b.control
	}
	return nil
}

// Hijacked: порт 53 перехвачен - за любой адрес отвечает чужой резолвер; с привязкой ответ - про путь с интерфейса.
func Hijacked() bool {
	return bound.Load() == nil && plainHijacked()
}

// Resolver: системный резолвер; при перехвате порта 53 - через DoH, с привязкой - с интерфейса к публичным резолверам.
func Resolver() *net.Resolver {
	if b := bound.Load(); b != nil {
		return b.resolver
	}
	if !plainHijacked() {
		return net.DefaultResolver
	}
	return dohResolver
}

// Exchange: один вопрос серверу "адрес:порт"; при перехвате порта 53 с рекурсией он идёт по DoH, без неё - ошибка.
func Exchange(server, name string, t dnsmessage.Type, rd bool) ([]byte, error) {
	msg, err := compose(name, t, rd, false)
	if err != nil {
		return nil, fmt.Errorf("вопрос про %q не собран: %w", name, err)
	}
	if !Hijacked() {
		return ask(guarded(Control()), server, msg, tries)
	}
	if !rd {
		return nil, errHijacked
	}
	return overHTTPS(context.Background(), msg)
}

// open: сокет без охраны webnet - для постоянных адресов: публичные резолверы и проба перехвата.
func open(control func(network, address string, c syscall.RawConn) error) dialing {
	return func(limit time.Duration) *net.Dialer {
		return &net.Dialer{Timeout: limit, Control: control}
	}
}

// guarded: сокет через общий вход webnet - адрес сервера зоны называет проверяемый домен.
func guarded(control func(network, address string, c syscall.RawConn) error) dialing {
	return func(limit time.Duration) *net.Dialer {
		d := webnet.Dialer(limit)
		guard := d.ControlContext
		if control == nil {
			return d
		}
		d.ControlContext = func(ctx context.Context, network, address string, c syscall.RawConn) error {
			if guard != nil {
				if err := guard(ctx, network, address, c); err != nil {
					return err
				}
			}
			return control(network, address, c)
		}
		return d
	}
}

// relayed: резолвер Go, который не ходит в сеть сам, а отдаёт каждый запрос функции send.
func relayed(send func(ctx context.Context, msg []byte) ([]byte, error)) *net.Resolver {
	return &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return &relay{ctx: ctx, send: send}, nil
	}}
}

// fitting: попытки, которые успевают до срока ctx; последняя из них укорочена.
func fitting(ctx context.Context, waits []time.Duration) []time.Duration {
	deadline, ok := ctx.Deadline()
	if !ok {
		return waits
	}
	left := time.Until(deadline)
	var out []time.Duration
	for _, wait := range waits {
		if left <= 0 {
			break
		}
		out = append(out, min(wait, left))
		left -= wait
	}
	return out
}

// reachable: отвечает ли на этом пути хоть один публичный резолвер.
func reachable(via dialing) error {
	msg, err := compose(".", dnsmessage.TypeNS, true, false)
	if err != nil {
		return err
	}
	last := errNoResolvers
	for _, server := range Resolvers {
		if _, err := overUDP(via, server, msg, bindTries); err != nil {
			last = err
			continue
		}
		return nil
	}
	return last
}

// intercepted: на вопрос к адресу, где сервера нет, пришёл ответ.
func intercepted(via dialing, waits []time.Duration) bool {
	msg, err := build(".", dnsmessage.TypeNS)
	if err != nil {
		return false
	}
	_, err = overUDP(via, probe, msg, waits)
	return err == nil
}

// ask: вопрос по UDP, обрезанный ответ - заново по TCP.
func ask(via dialing, server string, msg []byte, waits []time.Duration) ([]byte, error) {
	resp, err := overUDP(via, server, msg, waits)
	if err != nil || !truncated(resp) {
		return resp, err
	}
	return overTCP(via, server, msg)
}

// overUDP: попытки идут через один сокет - на прямом пути новый поток теряется чаще, чем пакет в старом.
func overUDP(via dialing, server string, msg []byte, waits []time.Duration) ([]byte, error) {
	conn, err := via(tcpWait).Dial("udp", server)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	last := errors.New("нет ответа")
	for _, wait := range waits {
		_ = conn.SetDeadline(time.Now().Add(wait))
		if _, err := conn.Write(msg); err != nil {
			last = err
			continue
		}
		resp, err := readReply(conn, msg)
		if err != nil {
			last = err
			continue
		}
		return resp, nil
	}
	return nil, last
}

// readReply ждёт ответ на свой вопрос до срока сокета; чужое и битое пропускает.
func readReply(conn net.Conn, query []byte) ([]byte, error) {
	buf := make([]byte, 4096)
	for {
		k, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if matches(query, buf[:k]) {
			return buf[:k], nil
		}
	}
}

func overTCP(via dialing, server string, msg []byte) ([]byte, error) {
	deadline := time.Now().Add(tcpWait)
	conn, err := via(tcpWait).Dial("tcp", server)
	if err != nil {
		return nil, fmt.Errorf("ответ по UDP обрезан, а по TCP сервер недоступен: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline)
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(msg)))
	if _, err := conn.Write(append(framed, msg...)); err != nil {
		return nil, fmt.Errorf("вопрос по TCP не ушёл: %w", err)
	}
	var length [2]byte
	if _, err := io.ReadFull(conn, length[:]); err != nil {
		return nil, fmt.Errorf("ответ по TCP не пришёл: %w", err)
	}
	resp := make([]byte, binary.BigEndian.Uint16(length[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, fmt.Errorf("ответ по TCP оборван: %w", err)
	}
	if !matches(msg, resp) {
		return nil, errors.New("по TCP пришёл ответ на другой вопрос")
	}
	return resp, nil
}

// matches: resp отвечает на query - тот же номер, а вопрос, если он в ответе повторён, тот же без учёта регистра.
func matches(query, resp []byte) bool {
	var asked, got dnsmessage.Parser
	qh, err := asked.Start(query)
	if err != nil {
		return false
	}
	rh, err := got.Start(resp)
	if err != nil || !rh.Response || rh.ID != qh.ID {
		return false
	}
	want, err := asked.Question()
	if err != nil {
		return false
	}
	have, err := got.Question()
	if errors.Is(err, dnsmessage.ErrSectionDone) {
		return true
	}
	return err == nil && have.Type == want.Type && strings.EqualFold(have.Name.String(), want.Name.String())
}

// truncated: бит TC - ответ не поместился или сервер ограничивает частоту; коду такого ответа верить нельзя.
func truncated(resp []byte) bool {
	return len(resp) > 2 && resp[2]&0x02 != 0
}
