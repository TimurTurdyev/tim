package dnsq

import (
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	typeAXFR dnsmessage.Type = 252
	// zoneLimit: дальше зону не читаем, открыта она и так.
	zoneLimit = 1 << 20
	zoneNames = 5
)

var (
	// ErrRefused: сервер зону не отдал.
	ErrRefused = errors.New("сервер зону не отдаёт")
	// ErrNotZone: ответил не сервер зоны, а кто-то на пути: в ответе нет признака, что он за неё отвечает.
	ErrNotZone = errors.New("ответил не сервер зоны")
)

// Transfer: что сервер отдал на просьбу прислать всю зону. Записей - сколько прочли; Cut - дочитывать не стали.
type Transfer struct {
	Records int
	Names   []string // первые имена
	Cut     bool
}

// AskZone просит у сервера имён всю зону по уже открытому соединению TCP.
func AskZone(conn io.ReadWriter, zone string) (Transfer, error) {
	msg, err := build(zone, typeAXFR)
	if err != nil {
		return Transfer{}, err
	}
	framed := binary.BigEndian.AppendUint16(nil, uint16(len(msg)))
	if _, err := conn.Write(append(framed, msg...)); err != nil {
		return Transfer{}, err
	}
	return ReadZone(conn)
}

// ReadZone читает ответ на просьбу отдать зону: сообщения с длиной впереди, первая и последняя запись - SOA.
func ReadZone(r io.Reader) (Transfer, error) {
	var t Transfer
	soa, size := 0, 0
	for soa < 2 {
		var length [2]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			if t.Records > 1 { // оборвалось посреди зоны - она всё равно открыта
				t.Cut = true
				return t, nil
			}
			return Transfer{}, ErrRefused
		}
		msg := make([]byte, binary.BigEndian.Uint16(length[:]))
		if _, err := io.ReadFull(r, msg); err != nil {
			if t.Records > 1 {
				t.Cut = true
				return t, nil
			}
			return Transfer{}, ErrRefused
		}
		n, err := t.read(msg)
		if err != nil {
			return Transfer{}, err
		}
		soa += n
		if size += len(msg); size > zoneLimit {
			t.Cut = true
			break
		}
	}
	// Одна SOA туда и обратно - это ответ "изменений нет", а не зона.
	if t.Records < 3 {
		return Transfer{}, ErrRefused
	}
	return t, nil
}

// read: записи одного сообщения; возвращает, сколько среди них SOA.
func (t *Transfer) read(msg []byte) (soa int, err error) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		return 0, ErrRefused
	}
	if h.RCode != dnsmessage.RCodeSuccess {
		return 0, ErrRefused
	}
	if err := p.SkipAllQuestions(); err != nil {
		return 0, ErrRefused
	}
	for {
		a, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			return soa, nil
		}
		if err != nil {
			return soa, ErrRefused
		}
		if t.Records == 0 {
			if a.Type != dnsmessage.TypeSOA {
				return 0, ErrRefused
			}
			if !h.Authoritative {
				return 0, ErrNotZone
			}
		}
		t.Records++
		if a.Type == dnsmessage.TypeSOA {
			soa++
		}
		name := strings.TrimSuffix(a.Name.String(), ".")
		if len(t.Names) < zoneNames && a.Type != dnsmessage.TypeSOA && !slices.Contains(t.Names, name) {
			t.Names = append(t.Names, name)
		}
		if err := p.SkipAnswer(); err != nil {
			return soa, ErrRefused
		}
	}
}
