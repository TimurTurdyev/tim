// Package netbase: локальные таблицы "адрес -> сеть, AS, имя AS, страна сервера"; сам ничего не качает.
package netbase

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"path/filepath"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// ErrNoTables: в каталоге нет полного набора таблиц.
var ErrNoTables = errors.New("локальных таблиц сетей нет")

// Info: Name - "КОРОТКОЕ - Название, CC", Registered - страна регистрации AS, Country - где стоит сервер.
type Info struct {
	ASN, Prefix, Name, Registered, Country string
	MOAS                                   bool // сеть объявляют несколько AS, названа сильнейшая по числу пиров
}

// Tables: открытые таблицы каталога. После Open не меняются, искать можно из нескольких горутин.
type Tables struct {
	all   [tableCount]table
	built time.Time
}

// Lookup: false - адрес не разобран или о нём ничего нет; сеть и страна бывают одна без другой.
func (t *Tables) Lookup(ip string) (Info, bool) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return Info{}, false
	}
	addr = addr.Unmap()
	nets, countries := &t.all[as4], &t.all[cc4]
	if addr.Is6() {
		nets, countries = &t.all[as6], &t.all[cc6]
	}
	key := addr.AsSlice()
	var info Info
	if val, ok := nets.covering(key); ok {
		if prefix, err := addr.Prefix(int(val[4])); err == nil {
			asn := val[:4]
			info.ASN = strconv.FormatUint(uint64(binary.BigEndian.Uint32(asn)), 10)
			info.Prefix = prefix.String()
			info.MOAS = val[5]&flagMOAS != 0
			info.Name, info.Registered = t.name(asn)
		}
	}
	if val, ok := countries.covering(key); ok {
		info.Country = countryCode(val)
	}
	return info, info != Info{}
}

// Built: когда таблицы собраны; у набора из разных сборок - время самой старой.
func (t *Tables) Built() time.Time { return t.built }

func (t *Tables) name(asn []byte) (name, registered string) {
	val, ok := t.all[names].exact(asn)
	if !ok {
		return "", ""
	}
	return ui.Plain(t.all[names].line(binary.BigEndian.Uint32(val))), countryCode(val[4:])
}

// Open читает таблицы каталога целиком. Нет хотя бы одной - ErrNoTables.
func Open(dir string) (*Tables, error) {
	t := &Tables{}
	for i, spec := range specs {
		tbl, err := readTable(filepath.Join(dir, spec.file), spec)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil, ErrNoTables
		case err != nil:
			return nil, fmt.Errorf("таблица %s: %w", spec.file, err)
		}
		t.all[i] = tbl
		if t.built.IsZero() || tbl.built.Before(t.built) {
			t.built = tbl.built
		}
	}
	return t, nil
}

// countryCode: две заглавные латинские буквы; всё остальное - "неизвестно".
func countryCode(val []byte) string {
	if !isCountry(val) {
		return ""
	}
	return string(val)
}

func isCountry(code []byte) bool {
	return len(code) == 2 && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z'
}
