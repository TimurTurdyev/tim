package servers

import (
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/TimurTurdyev/tim/internal/dnsbl"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/sshx"
)

// collectFull: заход по ssh и проверки снаружи (ssh без входа, чёрные списки) одновременно; порты Docker
// пробуются после отчёта - какие публикуются, видно только из него.
func collectFull(target string) (srvinfo.Report, error) {
	var o srvinfo.Outside
	ip := ""
	done := make(chan struct{})
	go func() {
		defer close(done)
		o = srvinfo.ProbeSSH(target)
		if ip = dnsbl.Resolve(sshx.Resolve(target).Host); ip != "" {
			o.Blacklist = dnsbl.Check(ip)
		}
	}()
	r, err := srvinfo.Collect(target, serverTimeout)
	<-done
	if err != nil {
		return r, err
	}
	o.Ports = probePorts(ip, r)
	r.Outside = &o
	return r, nil
}

// probePorts: отвечают ли снаружи порты, которые Docker публикует на всех адресах при включённом ufw.
func probePorts(ip string, r srvinfo.Report) map[int]bool {
	if ip == "" || r.UFW != "active" {
		return nil
	}
	out := map[int]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, p := range r.Watch.Docker {
		if p.Addr != "0.0.0.0" && p.Addr != "::" && p.Addr != "" {
			continue
		}
		wg.Go(func() {
			c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, strconv.Itoa(p.Port)), 3*time.Second)
			if err == nil {
				_ = c.Close()
			}
			mu.Lock()
			out[p.Port] = err == nil
			mu.Unlock()
		})
	}
	wg.Wait()
	return out
}
