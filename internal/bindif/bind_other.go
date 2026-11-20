//go:build !darwin

package bindif

import "syscall"

// Control: без root сокет к интерфейсу здесь не привязать.
func Control(string) (func(network, address string, c syscall.RawConn) error, error) {
	return nil, ErrUnsupported
}
