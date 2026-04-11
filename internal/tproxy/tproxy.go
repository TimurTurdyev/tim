// Package tproxy: прозрачный TCP-прокси для обхода VPN по приложениям. pf заворачивает соединения
// процессов группы novpn на lo0 и дальше на этот прокси (rdr); прокси узнаёт у pf настоящего
// адресата и ходит к нему сам - сокетом, привязанным к физическому интерфейсу, то есть мимо
// туннеля. Приложение ничего не замечает: для него соединение обычное.
package tproxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// Options: куда слушать и через что ходить наружу.
type Options struct {
	Listen string // 127.0.0.1:7788
	Iface  string // en0
	Local  net.IP // адрес en0 - источник исходящих соединений
	Logf   func(format string, a ...any)
}

// Serve слушает до отмены контекста. Нужен root: адресата спрашиваем у /dev/pf.
func Serve(ctx context.Context, o Options) error {
	pf, err := openPF()
	if err != nil {
		return err
	}
	defer func() { _ = pf.Close() }()
	ifi, err := net.InterfaceByName(o.Iface)
	if err != nil {
		return fmt.Errorf("интерфейс %s: %w", o.Iface, err)
	}
	ln, err := net.Listen("tcp4", o.Listen)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	dialer := &net.Dialer{Timeout: 15 * time.Second, LocalAddr: &net.TCPAddr{IP: o.Local}, Control: bindIface(ifi.Index)}
	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go relay(ctx, pf, dialer, c, logf)
	}
}

func relay(ctx context.Context, pf *os.File, d *net.Dialer, c net.Conn, logf func(string, ...any)) {
	defer func() { _ = c.Close() }()
	src, _ := c.RemoteAddr().(*net.TCPAddr)
	dst, _ := c.LocalAddr().(*net.TCPAddr)
	if src == nil || dst == nil {
		return
	}
	target, err := OriginalDst(pf, src, dst)
	if err != nil {
		logf("%s: адресат не найден: %v", src, err)
		return
	}
	up, err := d.DialContext(ctx, "tcp4", target)
	if err != nil {
		logf("%s -> %s: %v", src, target, err)
		return
	}
	defer func() { _ = up.Close() }()
	done := make(chan struct{}, 2)
	pipe := func(w, r net.Conn) {
		_, _ = io.Copy(w, r)
		if t, ok := w.(*net.TCPConn); ok {
			_ = t.CloseWrite()
		}
		done <- struct{}{}
	}
	go pipe(up, c)
	go pipe(c, up)
	<-done
	<-done
}
