package netbase

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net/netip"
	"strconv"
	"strings"
)

// parseCountry: "начало,конец,страна" по возрастанию; строка, которая заходит на прошлый диапазон, битая.
func parseCountry(r io.Reader, keyLen int) (section, error) {
	var sec section
	out := bounds{keyLen: keyLen, valLen: countryValLen}
	var prev netip.Addr
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		sec.lines++
		from, to, code, ok := parseRange(line, keyLen)
		if !ok || prev.IsValid() && from.Compare(prev) <= 0 {
			sec.broken++
			continue
		}
		out.put(from.AsSlice(), code)
		if next := to.Next(); next.IsValid() {
			out.put(next.AsSlice(), make([]byte, countryValLen))
		}
		prev = to
		sec.rows++
	}
	sec.keys, sec.vals = out.keys, out.vals
	return sec, sc.Err()
}

func parseRange(line []byte, keyLen int) (from, to netip.Addr, code []byte, ok bool) {
	first, rest, _ := bytes.Cut(line, []byte{','})
	last, code, _ := bytes.Cut(rest, []byte{','})
	from, fromErr := netip.ParseAddr(string(first))
	to, toErr := netip.ParseAddr(string(last))
	ok = fromErr == nil && toErr == nil && from.BitLen() == keyLen*8 && to.BitLen() == keyLen*8 &&
		from.Compare(to) <= 0 && isCountry(code)
	return from, to, code, ok
}

// parseNames разбирает asn.txt: "номер имя, страна" по возрастанию номера.
func parseNames(r io.Reader) (section, error) {
	text, err := io.ReadAll(r)
	if err != nil {
		return section{}, err
	}
	orgs := orgNames(text)
	var sec section
	prev := int64(-1)
	for raw := range bytes.Lines(text) {
		line := strings.TrimSpace(string(raw))
		if line == "" {
			continue
		}
		sec.lines++
		number, rest, _ := strings.Cut(line, " ")
		asn, err := strconv.ParseUint(number, 10, 32)
		if err != nil || int64(asn) <= prev {
			sec.broken++
			continue
		}
		prev = int64(asn)
		name, registered := asName(rest, orgs)
		var code [2]byte
		copy(code[:], registered)
		sec.keys = binary.BigEndian.AppendUint32(sec.keys, uint32(asn))
		sec.vals = binary.BigEndian.AppendUint32(sec.vals, uint32(len(sec.text)))
		sec.vals = append(sec.vals, code[:]...)
		sec.text = append(append(sec.text, name...), '\n')
		sec.rows++
	}
	return sec, nil
}

// orgNames: названия, стоящие за коротким именем; строка, равная такому названию целиком, сама без короткого имени.
func orgNames(text []byte) map[string]struct{} {
	orgs := map[string]struct{}{}
	for raw := range bytes.Lines(text) {
		_, rest, _ := strings.Cut(strings.TrimSpace(string(raw)), " ")
		body := squeeze(rest)
		if _, org, ok := strings.Cut(body, " "); ok && !separated(body) {
			orgs[org] = struct{}{}
		}
	}
	return orgs
}

// asName: имя в виде Cymru "КОРОТКОЕ - Название, CC" и страна из хвоста; разделителя нет - ставится за первым словом.
func asName(raw string, orgs map[string]struct{}) (name, registered string) {
	body := squeeze(raw)
	_, orgOnly := orgs[body]
	if tail := len(body) - 4; tail >= 0 && body[tail:tail+2] == ", " && isCountry([]byte(body[tail+2:])) {
		body, registered = body[:tail], body[tail+2:]
	}
	body = strings.TrimSpace(body)
	if body == "-" || strings.HasSuffix(body, " -") {
		body = strings.TrimSpace(strings.TrimSuffix(body, "-")) // короткое имя есть, а названия за ним нет
	}
	if short, org, ok := strings.Cut(body, " "); ok && !orgOnly && !separated(body) {
		body = short + " - " + org
	}
	if body == "" || registered == "" {
		return body, registered
	}
	return body + ", " + registered, registered
}

func squeeze(s string) string { return strings.Join(strings.Fields(s), " ") }

// separated: в имени есть " - " вне кавычек, то есть короткое имя уже отделено от названия.
func separated(body string) bool {
	for at := 0; ; {
		found := strings.Index(body[at:], " - ")
		if found < 0 {
			return false
		}
		at += found
		if strings.Count(body[:at], `"`)%2 == 0 {
			return true
		}
		at++
	}
}
