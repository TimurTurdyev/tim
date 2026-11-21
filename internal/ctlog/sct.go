package ctlog

import (
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// sctHead: версия, журнал, время и длина расширений в начале метки.
const sctHead = 1 + 32 + 8 + 2

var (
	sctOID = asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 11129, 2, 4, 2}
	errSCT = errors.New("список меток сертификата оборван")
)

// Anchor: метка сертификата - в какой журнал и когда его записали.
type Anchor struct {
	LogID    string // как log_id в списке журналов
	Time     time.Time
	Index    int64 // номер записи в журнале; -1 - в метке его нет
	NotAfter time.Time
	Names    []string // имена самого сертификата
}

// Anchors: метки сертификата. Время выпуска берётся только из них: NotBefore бывает на час раньше.
func Anchors(cert *x509.Certificate) []Anchor {
	if cert == nil {
		return nil
	}
	var names []string
	for _, name := range cert.DNSNames {
		if name = hostName(name); name != "" && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	var out []Anchor
	for _, ext := range cert.Extensions {
		if !ext.Id.Equal(sctOID) {
			continue
		}
		marks, _ := parseSCTs(ext.Value)
		for _, m := range marks {
			m.NotAfter, m.Names = cert.NotAfter, names
			out = append(out, m)
		}
	}
	return out
}

// parseSCTs: значение расширения - OCTET STRING со списком меток в кодировке TLS; при обрыве отдаёт разобранное.
func parseSCTs(ext []byte) ([]Anchor, error) {
	var list []byte
	if _, err := asn1.Unmarshal(ext, &list); err != nil {
		return nil, fmt.Errorf("метки сертификата: %w", err)
	}
	if len(list) < 2 || int(binary.BigEndian.Uint16(list)) != len(list)-2 {
		return nil, errSCT
	}
	var out []Anchor
	for list = list[2:]; len(list) > 0; {
		if len(list) < 2 {
			return out, errSCT
		}
		size := int(binary.BigEndian.Uint16(list))
		if size < sctHead || len(list) < 2+size {
			return out, errSCT
		}
		mark := list[2 : 2+size]
		list = list[2+size:]
		if mark[0] != 0 {
			continue
		}
		extSize := int(binary.BigEndian.Uint16(mark[sctHead-2:]))
		if len(mark) < sctHead+extSize {
			return out, errSCT
		}
		out = append(out, Anchor{
			LogID: base64.StdEncoding.EncodeToString(mark[1:33]),
			Time:  time.UnixMilli(int64(binary.BigEndian.Uint64(mark[33:41]))).UTC(),
			Index: leafIndex(mark[sctHead : sctHead+extSize]),
		})
	}
	return out, nil
}

// leafIndex: номер записи из расширений метки плиточного журнала (тип 0, пять байт); -1 - его нет.
func leafIndex(ext []byte) int64 {
	for len(ext) >= 3 {
		size := int(binary.BigEndian.Uint16(ext[1:3]))
		if len(ext) < 3+size {
			break
		}
		if ext[0] == 0 && size == 5 {
			return int64(ext[3])<<32 | int64(binary.BigEndian.Uint32(ext[4:8]))
		}
		ext = ext[3+size:]
	}
	return -1
}

// hostName: имя из сертификата или плитки в виде для сравнения - нижний регистр, без "*."; пусто - не имя.
func hostName(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(strings.ToLower(ui.Plain(s)), "*."), ".")
	if strings.Contains(s, " ") {
		return ""
	}
	return s
}

// inDomain: имя - сам домен или его поддомен.
func inDomain(name, domain string) bool {
	if name == domain {
		return true
	}
	cut := len(name) - len(domain) - 1
	return cut > 0 && name[cut] == '.' && name[cut+1:] == domain
}
