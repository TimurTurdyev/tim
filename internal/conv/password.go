package conv

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
)

const (
	lowers  = "abcdefghijklmnopqrstuvwxyz"
	uppers  = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	digits  = "0123456789"
	symbols = "!@#$%^&*()-_=+[]{}:,.?"
	similar = "0Oo1lI"
)

// Password: криптослучайный пароль длины n, в котором есть каждый включённый класс символов.
func Password(n int, useSymbols, noAmbiguous bool) (string, error) {
	classes := []string{lowers, uppers, digits}
	if useSymbols {
		classes = append(classes, symbols)
	}
	if n < len(classes) {
		return "", errors.New("пароль короче числа классов символов")
	}
	if noAmbiguous {
		for i, c := range classes {
			classes[i] = strings.Map(func(r rune) rune {
				if strings.ContainsRune(similar, r) {
					return -1
				}
				return r
			}, c)
		}
	}
	all := strings.Join(classes, "")
	out := make([]byte, n)
	for i, c := range classes {
		out[i] = pick(c)
	}
	for i := len(classes); i < n; i++ {
		out[i] = pick(all)
	}
	for i := n - 1; i > 0; i-- {
		j := randInt(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return string(out), nil
}

func pick(set string) byte { return set[randInt(len(set))] }

func randInt(n int) int {
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		panic(err)
	}
	return int(v.Int64())
}
