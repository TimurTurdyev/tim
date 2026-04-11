package tproxy

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// pfioc_natlook: struct pfioc_natlook из pfvar.h xnu (заголовок в SDK не входит, раскладка
// известная): четыре pf_addr по 16 байт, четыре pf_state_xport по 4 байта, af, proto,
// proto_variant, direction.
type natlook struct {
	saddr, daddr, rsaddr, rdaddr     [16]byte
	sxport, dxport, rsxport, rdxport [4]byte
	af, proto, protoVariant, dir     uint8
}

// DIOCNATLOOK = _IOWR('D', 23, struct pfioc_natlook), размер 84 байта.
const (
	diocNatLook = 0xc0544417
	pfOut       = 2
)

func openPF() (*os.File, error) {
	f, err := os.OpenFile("/dev/pf", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("/dev/pf: %w (нужен root)", err)
	}
	return f, nil
}

// OriginalDst: куда на самом деле шло соединение, которое rdr завернул на прокси.
func OriginalDst(pf *os.File, src, dst *net.TCPAddr) (string, error) {
	s4, d4 := src.IP.To4(), dst.IP.To4()
	if s4 == nil || d4 == nil {
		return "", errors.New("только IPv4")
	}
	var nl natlook
	copy(nl.saddr[:4], s4)
	copy(nl.daddr[:4], d4)
	binary.BigEndian.PutUint16(nl.sxport[:2], uint16(src.Port))
	binary.BigEndian.PutUint16(nl.dxport[:2], uint16(dst.Port))
	nl.af, nl.proto, nl.dir = unix.AF_INET, unix.IPPROTO_TCP, pfOut
	// Обёртки для DIOCNATLOOK в x/sys нет, остаётся сырой ioctl.
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, pf.Fd(), diocNatLook, uintptr(unsafe.Pointer(&nl))); errno != 0 { //nolint:staticcheck

		return "", errno
	}
	ip := net.IP(nl.rdaddr[:4])
	port := binary.BigEndian.Uint16(nl.rdxport[:2])
	return net.JoinHostPort(ip.String(), fmt.Sprint(port)), nil
}

// bindIface: исходящий сокет привязан к интерфейсу, и маршрут ищется в его рамках - мимо
// туннеля, даже если тот держит маршрут по умолчанию.
func bindIface(index int) func(network, address string, c syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
		})
		if err != nil {
			return err
		}
		return serr
	}
}
