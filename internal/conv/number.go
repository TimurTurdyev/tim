// Package conv: разбор и преобразование значений для утилит tim.
package conv

import (
	"fmt"
	"math/big"
	"strings"
)

// ParseNumber понимает префиксы 0x, 0b, 0o и знак; без префикса берётся base.
func ParseNumber(s string, base int) (*big.Int, error) {
	t := strings.ReplaceAll(strings.TrimSpace(s), "_", "")
	neg := false
	if strings.HasPrefix(t, "-") {
		neg, t = true, t[1:]
	}
	low := strings.ToLower(t)
	switch {
	case strings.HasPrefix(low, "0x"):
		base, t = 16, t[2:]
	case strings.HasPrefix(low, "0b"):
		base, t = 2, t[2:]
	case strings.HasPrefix(low, "0o"):
		base, t = 8, t[2:]
	}
	n, ok := new(big.Int).SetString(t, base)
	if !ok || t == "" {
		return nil, fmt.Errorf("не число в системе %d: %s", base, s)
	}
	if neg {
		n.Neg(n)
	}
	return n, nil
}

// Hex: 0xff или -0xff.
func Hex(n *big.Int) string {
	if n.Sign() < 0 {
		return "-0x" + new(big.Int).Neg(n).Text(16)
	}
	return "0x" + n.Text(16)
}

// Bin с группами по 4 бита, только для неотрицательных до 16 бит включительно.
func Bin(n *big.Int) (string, bool) {
	if n.Sign() < 0 || n.BitLen() > 16 {
		return "", false
	}
	s := n.Text(2)
	if pad := (4 - len(s)%4) % 4; pad > 0 {
		s = strings.Repeat("0", pad) + s
	}
	var parts []string
	for i := 0; i < len(s); i += 4 {
		parts = append(parts, s[i:i+4])
	}
	return "0b" + strings.Join(parts, "_"), true
}
