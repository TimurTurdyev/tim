package conv

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type JWT struct {
	Header json.RawMessage
	Claims json.RawMessage
	Times  map[string]time.Time
}

// DecodeJWT разбирает токен без проверки подписи.
func DecodeJWT(token string) (*JWT, error) {
	t := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(token), "Bearer "))
	parts := strings.Split(t, ".")
	if len(parts) != 3 {
		return nil, errors.New("JWT должен состоять из трёх частей через точку")
	}
	header, err := Base64Decode(parts[0])
	if err != nil {
		return nil, errors.New("header: " + err.Error())
	}
	claims, err := Base64Decode(parts[1])
	if err != nil {
		return nil, errors.New("payload: " + err.Error())
	}
	if !json.Valid(header) || !json.Valid(claims) {
		return nil, errors.New("header или payload не JSON")
	}
	j := &JWT{Header: header, Claims: claims, Times: map[string]time.Time{}}
	var m map[string]any
	dec := json.NewDecoder(bytes.NewReader(claims))
	dec.UseNumber()
	if err := dec.Decode(&m); err == nil {
		for _, k := range []string{"iat", "nbf", "exp"} {
			if n, ok := m[k].(json.Number); ok {
				if v, err := n.Float64(); err == nil {
					j.Times[k] = time.Unix(int64(v), 0)
				}
			}
		}
	}
	return j, nil
}

// Expired: exp в прошлом относительно now.
func (j *JWT) Expired(now time.Time) (bool, bool) {
	exp, ok := j.Times["exp"]
	if !ok {
		return false, false
	}
	return now.After(exp), true
}
