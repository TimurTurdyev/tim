package namebase

import (
	"crypto/md5"
	"encoding/hex"
	"hash"
	"strconv"
	"strings"
)

// Кусок, которым Common Crawl заливает части в хранилище: от него зависит ETag вида "md5-N".
const etagChunk = 8 << 20

// etagSum считает ETag хранилища по ходу чтения: md5 файла, а у залитого кусками - md5 от md5 кусков.
type etagSum struct {
	want  string
	chunk int // 0 - файл хешируется целиком
	fill  int
	cur   hash.Hash
	sums  []byte
}

func newETagSum(tag string) *etagSum {
	e := &etagSum{want: strings.ToLower(strings.Trim(strings.TrimPrefix(tag, "W/"), `"`)), cur: md5.New()}
	if strings.Contains(e.want, "-") {
		e.chunk = etagChunk
	}
	return e
}

func (e *etagSum) Write(p []byte) (int, error) {
	total := len(p)
	if e.chunk == 0 {
		e.cur.Write(p)
		return total, nil
	}
	for len(p) > 0 {
		n := min(len(p), e.chunk-e.fill)
		e.cur.Write(p[:n])
		e.fill += n
		p = p[n:]
		if e.fill == e.chunk {
			e.sums = e.cur.Sum(e.sums)
			e.cur.Reset()
			e.fill = 0
		}
	}
	return total, nil
}

// matches: прочитанное сошлось с ETag. Нет - ещё не порча: размер куска мог быть другим.
func (e *etagSum) matches() bool {
	if e.want == "" {
		return false
	}
	if e.chunk == 0 {
		return hex.EncodeToString(e.cur.Sum(nil)) == e.want
	}
	sums := e.sums
	if e.fill > 0 {
		sums = e.cur.Sum(sums)
	}
	all := md5.Sum(sums)
	return hex.EncodeToString(all[:])+"-"+strconv.Itoa(len(sums)/md5.Size) == e.want
}
