package bindif

import (
	"fmt"
	"net"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

// Control: функция для net.Dialer.Control и net.ListenConfig.Control - сокет TCP или UDP ходит через интерфейс iface.
func Control(iface string) (func(network, address string, c syscall.RawConn) error, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("интерфейс %s: %w", iface, err)
	}
	index := ifi.Index
	return func(network, _ string, c syscall.RawConn) error {
		level, option := unix.IPPROTO_IP, unix.IP_BOUND_IF
		// У сокета IPv6 свой параметр: IP_BOUND_IF на нём ядро отвергает.
		if strings.HasSuffix(network, "6") {
			level, option = unix.IPPROTO_IPV6, unix.IPV6_BOUND_IF
		}
		var failed error
		err := c.Control(func(fd uintptr) {
			failed = unix.SetsockoptInt(int(fd), level, option, index)
		})
		if err != nil {
			return err
		}
		return failed
	}, nil
}
