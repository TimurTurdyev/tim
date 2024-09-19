package sshx

import (
	"errors"
	"strconv"
	"strings"
)

// Forward: локальный порт и куда он ведёт на стороне сервера.
type Forward struct {
	Local, Host, Remote string
}

func (f Forward) Arg() string { return "127.0.0.1:" + f.Local + ":" + f.Host + ":" + f.Remote }

// ParseForward понимает "L:host:R", "L:R" и "R" (тогда L = R, host = localhost).
func ParseForward(spec string) (Forward, error) {
	parts := strings.Split(strings.TrimSpace(spec), ":")
	var f Forward
	switch len(parts) {
	case 1:
		f = Forward{parts[0], "localhost", parts[0]}
	case 2:
		f = Forward{parts[0], "localhost", parts[1]}
	case 3:
		f = Forward{parts[0], parts[1], parts[2]}
	default:
		return Forward{}, errors.New("проброс пишется как L:host:R, L:R или R: " + spec)
	}
	for _, p := range []string{f.Local, f.Remote} {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return Forward{}, errors.New("порт вне 1-65535: " + p)
		}
	}
	if f.Host == "" {
		return Forward{}, errors.New("пустой host в " + spec)
	}
	return f, nil
}
