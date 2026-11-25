package domaudit

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/ctlog"
	"github.com/TimurTurdyev/tim/internal/ui"
)

const (
	nearBudget = 30 * time.Second
	nearBytes  = 50 << 20
)

// near: имена, записанные в журналы сертификатов рядом с сертификатами найденных сайтов: сервер выпускает их
// пачкой. Нужно, когда сертификатные сервисы не спрашивали или они не ответили.
func (c *collector) near() {
	if c.opt.night || !c.opt.Quiet && c.certsKnown() {
		return
	}
	var anchors []ctlog.Anchor
	for _, cert := range c.certs {
		anchors = append(anchors, ctlog.Anchors(cert)...)
	}
	if len(anchors) == 0 {
		return
	}
	c.note("журналы сертификатов: что выпускали рядом")
	got := ctlog.Near(context.Background(), c.domain, anchors, ctlog.NearOptions{Budget: nearBudget, MaxBytes: nearBytes, Note: c.note})
	for _, n := range got.Names {
		c.learn(n, fromLog)
	}
	for _, a := range anchors {
		c.ranOver(fromLog, []string{a.LogID + " " + strconv.FormatInt(a.Index, 10)})
	}
	st := c.state(fromLog)
	st.Cut = got.Cut || got.Err != nil
	st.Note = "плиток " + strconv.Itoa(got.Tiles) + ", " + ui.Bytes(got.Bytes)
	if got.Cut {
		st.Note += ", прочитано не всё"
	}
	if got.Err != nil {
		st.Error = ui.Plain(ui.ShortErr(got.Err))
	}
}

// certsKnown: хотя бы один сторонний сервис с сертификатами ответил - соседей по журналам он знает и так.
func (c *collector) certsKnown() bool {
	if c.asking == nil {
		return false
	}
	found := c.asking.found()
	for i, s := range found.states {
		if c.asking.list[i].label == fromCert && (s.Error == "" || s.Names > 0) && !slices.Contains(c.asking.waiting(), s.Name) {
			return true
		}
	}
	return false
}
