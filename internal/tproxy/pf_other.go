//go:build !darwin

package tproxy

import (
	"errors"
	"net"
	"os"
	"syscall"
)

var errOnlyDarwin = errors.New("обход по приложениям через pf есть только на macOS")

func openPF() (*os.File, error) { return nil, errOnlyDarwin }

func OriginalDst(*os.File, *net.TCPAddr, *net.TCPAddr) (string, error) { return "", errOnlyDarwin }

func bindIface(int) func(network, address string, c syscall.RawConn) error { return nil }
