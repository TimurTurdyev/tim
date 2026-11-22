package netbase

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"net/netip"
	"sort"
	"strconv"
)

const (
	minPeers  = 10 // анонс, который видит меньше пиров RIS, - утечка или местный маршрут
	routeTail = 7  // в записи за адресом: длина сети, обратное число пиров, номер AS
)

// Строка источника: идёт в таблицу, разобрана, но не нужна, или не разобрана.
type lineKind int

const (
	lineKept lineKind = iota
	lineDropped
	lineBroken
)

type route struct {
	prefix netip.Prefix
	asn    uint32
	peers  int
}

// routes: анонсы одним срезом; порядок байтов записи - порядок сборки: внешняя сеть раньше, сильнейшая AS первой.
type routes struct {
	keyLen int
	data   []byte
}

func (r *routes) add(rt route) {
	r.data = append(r.data, rt.prefix.Addr().AsSlice()...)
	r.data = append(r.data, byte(rt.prefix.Bits()))
	r.data = binary.BigEndian.AppendUint16(r.data, ^uint16(min(rt.peers, math.MaxUint16)))
	r.data = binary.BigEndian.AppendUint32(r.data, rt.asn)
}

func (r *routes) at(i int) []byte {
	width := r.keyLen + routeTail
	return r.data[i*width : (i+1)*width]
}

// net: адрес и длина сети записи.
func (r *routes) net(i int) []byte { return r.at(i)[:r.keyLen+1] }

func (r *routes) asn(i int) []byte { return r.at(i)[r.keyLen+3:] }

func (r *routes) Len() int { return len(r.data) / (r.keyLen + routeTail) }

func (r *routes) Less(i, j int) bool { return bytes.Compare(r.at(i), r.at(j)) < 0 }

func (r *routes) Swap(i, j int) {
	var tmp [16 + routeTail]byte
	a, b := r.at(i), r.at(j)
	n := copy(tmp[:], a)
	copy(a, b)
	copy(b, tmp[:n])
}

// bounds копит границы отрезков: ключ - первый адрес отрезка, значение действует до следующего ключа.
type bounds struct {
	keyLen, valLen int
	keys, vals     []byte
}

// put ставит границу: повтор ключа заменяет значение, а значение как у соседа слева границы не даёт.
func (b *bounds) put(key, val []byte) {
	if last := len(b.keys) - b.keyLen; last >= 0 && bytes.Equal(b.keys[last:], key) {
		b.keys, b.vals = b.keys[:last], b.vals[:len(b.vals)-b.valLen]
	}
	if last := len(b.vals) - b.valLen; last >= 0 && bytes.Equal(b.vals[last:], val) {
		return
	}
	b.keys = append(b.keys, key...)
	b.vals = append(b.vals, val...)
}

type openNet struct {
	last [16]byte
	val  [asValLen]byte
}

// sweep идёт по сетям от внешней к вложенной: за концом вложенной отрезок возвращается внешней.
type sweep struct {
	out   bounds
	stack []openNet
}

func (s *sweep) open(addr []byte, bits int, val [asValLen]byte) {
	s.closeBefore(addr)
	s.out.put(addr, val[:])
	s.stack = append(s.stack, openNet{last: lastOf(addr, bits), val: val})
}

// closeBefore закрывает сети, которые кончились до адреса addr; nil - все оставшиеся.
func (s *sweep) closeBefore(addr []byte) {
	keyLen := s.out.keyLen
	for n := len(s.stack); n > 0; n = len(s.stack) {
		top := s.stack[n-1]
		if addr != nil && bytes.Compare(top.last[:keyLen], addr) >= 0 {
			return
		}
		s.stack = s.stack[:n-1]
		var outer [asValLen]byte
		if n > 1 {
			outer = s.stack[n-2].val
		}
		if next, ok := nextOf(top.last[:keyLen]); ok {
			s.out.put(next[:keyLen], outer[:])
		}
	}
}

func parseRIS(r io.Reader, keyLen int) (section, error) {
	var sec section
	list := routes{keyLen: keyLen}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 || line[0] == '%' {
			continue
		}
		sec.lines++
		rt, kind := parseRoute(line, keyLen)
		switch kind {
		case lineBroken:
			sec.broken++
		case lineKept:
			list.add(rt)
		}
	}
	if err := sc.Err(); err != nil {
		return section{}, err
	}
	sort.Sort(&list)
	sec.keys, sec.vals, sec.rows = flatten(&list)
	return sec, nil
}

// parseRoute: строка дампа "origin <TAB> сеть <TAB> число пиров".
func parseRoute(line []byte, keyLen int) (route, lineKind) {
	from, rest, hasNet := bytes.Cut(line, []byte{'\t'})
	network, seen, hasPeers := bytes.Cut(rest, []byte{'\t'})
	prefix, err := netip.ParsePrefix(string(network))
	peers, peersErr := strconv.Atoi(string(seen))
	if !hasNet || !hasPeers || err != nil || peersErr != nil || peers < 0 || prefix != prefix.Masked() ||
		prefix.Addr().BitLen() != keyLen*8 {
		return route{}, lineBroken
	}
	asn, kind := origin(from)
	if kind != lineKept {
		return route{}, kind
	}
	widest, narrowest := bitsRange(keyLen)
	if reserved(asn) || prefix.Bits() < widest || prefix.Bits() > narrowest || peers < minPeers {
		return route{}, lineDropped
	}
	return route{prefix: prefix, asn: asn, peers: peers}, lineKept
}

// origin: номер AS или набор из одной AS в скобках; набор из нескольких - не владелец сети.
func origin(field []byte) (uint32, lineKind) {
	if len(field) > 2 && field[0] == '{' && field[len(field)-1] == '}' {
		field = field[1 : len(field)-1]
		if bytes.IndexByte(field, ',') >= 0 {
			return 0, lineDropped
		}
	}
	asn, err := strconv.ParseUint(string(field), 10, 32)
	if err != nil {
		return 0, lineBroken
	}
	return uint32(asn), lineKept
}

// reserved: запасные, частные и учебные номера - у настоящего владельца сети таких не бывает.
func reserved(asn uint32) bool {
	return asn == 0 || asn == 23456 || asn >= 64496 && asn <= 131071 || asn >= 4200000000
}

// bitsRange: шире - маршруты по умолчанию и ошибки, уже - такие анонсы интернет целиком не принимает.
func bitsRange(keyLen int) (widest, narrowest int) {
	if keyLen == 4 {
		return 8, 24
	}
	return 16, 48
}

// flatten разворачивает вложенные сети в отрезки с самой узкой сетью; другая AS у той же сети - признак MOAS.
func flatten(list *routes) (keys, vals []byte, nets int) {
	sw := sweep{out: bounds{keyLen: list.keyLen, valLen: asValLen}}
	n := list.Len()
	for i := 0; i < n; {
		net, asn := list.net(i), list.asn(i)
		var val [asValLen]byte
		copy(val[:], asn)
		val[4] = net[list.keyLen]
		for i++; i < n && bytes.Equal(list.net(i), net); i++ {
			if !bytes.Equal(list.asn(i), asn) {
				val[5] = flagMOAS
			}
		}
		sw.open(net[:list.keyLen], int(val[4]), val)
		nets++
	}
	sw.closeBefore(nil)
	return sw.out.keys, sw.out.vals, nets
}

// lastOf: последний адрес сети - биты хоста единицами.
func lastOf(addr []byte, bits int) [16]byte {
	var last [16]byte
	copy(last[:], addr)
	for i := bits; i < len(addr)*8; i++ {
		last[i/8] |= 1 << (7 - i%8)
	}
	return last
}

// nextOf: адрес сразу за данным; false - дальше адресов нет.
func nextOf(addr []byte) ([16]byte, bool) {
	var next [16]byte
	copy(next[:], addr)
	for i := len(addr) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			return next, true
		}
	}
	return next, false
}
