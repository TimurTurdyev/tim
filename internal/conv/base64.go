package conv

import (
	"encoding/base64"
	"errors"
	"strings"
)

// Base64Decode сам выбирает std или url-алфавит и терпит отсутствие паддинга.
func Base64Decode(s string) ([]byte, error) {
	t := strings.Join(strings.Fields(s), "")
	t = strings.TrimRight(t, "=")
	enc := base64.RawStdEncoding
	if strings.ContainsAny(t, "-_") {
		enc = base64.RawURLEncoding
	}
	out, err := enc.DecodeString(t)
	if err != nil {
		return nil, errors.New("не base64: " + err.Error())
	}
	return out, nil
}
