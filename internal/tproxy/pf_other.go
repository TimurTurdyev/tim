//go:build !darwin

package tproxy

import (
	"errors"
	"net"
	"os"
)

var errOnlyDarwin = errors.New("обход по приложениям через pf есть только на macOS")

func openPF() (*os.File, error) { return nil, errOnlyDarwin }

func OriginalDst(*os.File, *net.TCPAddr, *net.TCPAddr) (string, error) { return "", errOnlyDarwin }
