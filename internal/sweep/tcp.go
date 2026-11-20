package sweep

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"time"
)

const (
	tcpWait = 3 * time.Second
	// tcpStrikes: столько новых соединений подряд без единого ответа - TCP у сервера считается нерабочим
	tcpStrikes = 2
)

// lane: очередь TCP одного сервера - на TCP ограничение частоты ответов не действует.
func (d *direct) lane(ctx context.Context, n int) {
	var conn net.Conn
	var batch []int32
	// twice: имена, которые уже остались без ответа на одном соединении
	twice := map[int32]bool{}
	// fresh: соединение ещё ни разу не ответило
	fresh, strikes := true, 0
	for strikes < tcpStrikes {
		if batch = d.fill(ctx, n, batch); len(batch) == 0 {
			break
		}
		if conn == nil {
			c, err := d.dialTCP(ctx, n)
			if err != nil {
				strikes++
				continue
			}
			conn, fresh = c, true
		}
		left, err := d.exchange(ctx, conn, batch)
		silent := len(left) == len(batch)
		if err != nil {
			_ = conn.Close()
			conn = nil
		}
		switch {
		case !silent:
			fresh, strikes = false, 0
			batch = d.second(left, twice)
		case fresh:
			// новое соединение без единого ответа - не вина имён
			strikes++
			batch = left
		default:
			// старое соединение сервер закрывает сам: после сотни вопросов или по простою
			batch = left
		}
	}
	if conn != nil {
		_ = conn.Close()
	}
	if strikes == tcpStrikes {
		d.mu.Lock()
		d.eng.tcpLost(n, batch)
		d.mu.Unlock()
	}
}

func (d *direct) dialTCP(ctx context.Context, n int) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, tcpWait)
	defer cancel()
	return d.dialer.DialContext(ctx, "tcp", d.addrs[n])
}

// second: имя без ответа получает ещё одно соединение; не ответили и там - возвращается к запросам UDP.
func (d *direct) second(left []int32, twice map[int32]bool) []int32 {
	var keep []int32
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, idx := range left {
		if twice[idx] {
			d.eng.tcpAnswer(idx, Answer{}, time.Since(d.start))
			continue
		}
		twice[idx] = true
		keep = append(keep, idx)
	}
	return keep
}

// fill: добирает имена до окна; пока их нет - ждёт сигнала. Пусто - перебор кончился.
func (d *direct) fill(ctx context.Context, n int, batch []int32) []int32 {
	for {
		d.mu.Lock()
		batch = append(batch, d.eng.takeTCP(n, tcpWindow-len(batch))...)
		d.mu.Unlock()
		if len(batch) > 0 && ctx.Err() == nil {
			return batch
		}
		select {
		case <-ctx.Done():
			return nil
		case <-d.wake[n]:
		}
	}
}

// exchange: вопросы подряд по одному соединению, не дожидаясь ответов. Возвращает имена без ответа.
func (d *direct) exchange(ctx context.Context, conn net.Conn, batch []int32) ([]int32, error) {
	_ = conn.SetDeadline(time.Now().Add(tcpWait))
	// отмена ставится после срока ответа: иначе срок затёр бы отмену, которая уже пришла
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	var out []byte
	for i, idx := range batch {
		msg, err := buildQuery(nil, uint16(i), d.eng.name(idx), d.opt.Type, d.opt.Recursive)
		if err != nil {
			continue
		}
		out = binary.BigEndian.AppendUint16(out, uint16(len(msg)))
		out = append(out, msg...)
	}
	if _, err := conn.Write(out); err != nil {
		return batch, err
	}
	answered := make([]bool, len(batch))
	for open := len(batch); open > 0; {
		msg, err := readFrame(conn)
		if err != nil {
			return unanswered(batch, answered), err
		}
		id, _, ok := question(msg)
		if !ok || int(id) >= len(batch) || answered[id] {
			continue
		}
		a, truncated := Classify(msg, d.eng.name(batch[id]))
		if a.Kind == None && !truncated {
			continue
		}
		answered[id] = true
		open--
		d.mu.Lock()
		d.eng.tcpAnswer(batch[id], a, time.Since(d.start))
		d.mu.Unlock()
	}
	return nil, nil
}

// readFrame: сообщение DNS по TCP - два байта длины и тело.
func readFrame(r io.Reader) ([]byte, error) {
	var size [2]byte
	if _, err := io.ReadFull(r, size[:]); err != nil {
		return nil, err
	}
	msg := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(r, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func unanswered(batch []int32, answered []bool) []int32 {
	var left []int32
	for i, idx := range batch {
		if !answered[i] {
			left = append(left, idx)
		}
	}
	return left
}
