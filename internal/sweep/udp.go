package sweep

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const (
	// recvBuffer: на частоте в тысячи ответов в секунду обычный буфер сокета переполняется
	recvBuffer  = 4 << 20
	tickEvery   = 2 * time.Millisecond
	dialTimeout = 5 * time.Second
	dnsPort     = "53"
)

// direct: перебор по UDP - один подключённый сокет на сервер на весь прогон, обрезанные ответы - по TCP.
type direct struct {
	opt    Options
	dialer *net.Dialer
	start  time.Time
	addrs  []string
	conns  []net.Conn
	// wake: сигнал очереди TCP сервера - появились имена
	wake []chan struct{}
	mu   sync.Mutex
	eng  *engine
}

func overUDP(ctx context.Context, names []string, opt Options, start time.Time) Result {
	d := &direct{opt: opt, dialer: &net.Dialer{Timeout: dialTimeout, Control: opt.Control}, start: start}
	d.connect(ctx)
	if len(d.conns) == 0 {
		return Result{}
	}
	d.eng = newEngine(names, len(d.conns), opt.Recursive, horizon(ctx, start, opt.Budget))
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	for n := range d.conns {
		wg.Go(func() { d.receive(n) })
		wg.Go(func() { d.lane(ctx, n) })
	}
	d.loop(ctx)
	cancel()
	for _, c := range d.conns {
		_ = c.Close()
	}
	wg.Wait()
	d.report()
	return d.eng.result()
}

// connect: сервер, до которого сокет не открылся, в перебор не идёт.
func (d *direct) connect(ctx context.Context) {
	for _, addr := range d.opt.Servers {
		if len(d.conns) == maxPeers {
			return
		}
		if _, _, err := net.SplitHostPort(addr); err != nil {
			addr = net.JoinHostPort(addr, dnsPort)
		}
		c, err := d.dialer.DialContext(ctx, "udp", addr)
		if err != nil {
			continue
		}
		if u, ok := c.(*net.UDPConn); ok {
			_ = u.SetReadBuffer(recvBuffer)
		}
		d.addrs, d.conns = append(d.addrs, addr), append(d.conns, c)
		d.wake = append(d.wake, make(chan struct{}, 1))
	}
}

func (d *direct) loop(ctx context.Context) {
	ticker := time.NewTicker(tickEvery)
	defer ticker.Stop()
	var shots []shot
	var shown time.Duration
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		now := time.Since(d.start)
		d.mu.Lock()
		shots = append(shots[:0], d.eng.tick(now)...)
		done := d.eng.finished(now)
		for n := range d.conns {
			d.nudge(n)
		}
		d.mu.Unlock()
		d.send(shots)
		if done {
			return
		}
		if now-shown >= progressEvery {
			shown = now
			d.report()
		}
	}
}

// send: ошибка записи - та же потеря, имя повторит движок.
func (d *direct) send(shots []shot) {
	buf := make([]byte, 0, 512)
	for _, s := range shots {
		msg, err := buildQuery(buf, s.id, d.eng.name(s.idx), d.opt.Type, d.opt.Recursive)
		if err != nil {
			continue
		}
		_, _ = d.conns[s.server].Write(msg)
	}
}

func (d *direct) receive(n int) {
	buf := make([]byte, 4096)
	for {
		size, err := d.conns[n].Read(buf)
		if errors.Is(err, net.ErrClosed) {
			return
		}
		if err != nil {
			// ICMP "порт недоступен" приходит ошибкой чтения, сокет при этом жив
			time.Sleep(time.Millisecond)
			continue
		}
		d.mu.Lock()
		d.eng.receive(n, buf[:size], time.Since(d.start))
		d.nudge(n)
		d.mu.Unlock()
	}
}

// nudge: будит очередь TCP сервера n, если в ней появились имена. Вызывается под d.mu.
func (d *direct) nudge(n int) {
	if !d.eng.tcpWaiting(n) {
		return
	}
	select {
	case d.wake[n] <- struct{}{}:
	default:
	}
}

func (d *direct) report() {
	if d.opt.Progress == nil {
		return
	}
	d.mu.Lock()
	done, total := d.eng.book.progress()
	d.mu.Unlock()
	d.opt.Progress(done, total)
}
