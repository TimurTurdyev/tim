package envfile

import (
	"net/url"
	"strings"
)

var secretWords = []string{"PASS", "SECRET", "TOKEN", "KEY", "PRIVATE", "SALT", "AUTH", "CREDENTIAL"}

// Secret: по имени ключа похоже на пароль или токен.
func Secret(key string) bool {
	k := strings.ToUpper(key)
	for _, w := range secretWords {
		if strings.Contains(k, w) {
			return true
		}
	}
	return false
}

// Mask прячет значение секретного ключа и пароль внутри URL вида scheme://user:pass@host.
func Mask(key, value string) string {
	if value == "" {
		return value
	}
	if Secret(key) {
		return "***"
	}
	if strings.Contains(value, "://") && strings.Contains(value, "@") {
		if u, err := url.Parse(value); err == nil && u.User != nil {
			if _, has := u.User.Password(); has {
				return u.Scheme + "://" + u.User.Username() + ":***@" + u.Host + u.RequestURI()
			}
		}
	}
	return value
}
