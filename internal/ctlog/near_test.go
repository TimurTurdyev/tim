package ctlog

import (
	"maps"
	"slices"
	"strings"
	"testing"
	"time"
)

func excerpt(t *testing.T) []logInfo {
	t.Helper()
	list, err := parseLogList(sample(t, "log-list-excerpt.json"))
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func date(t *testing.T, s string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// Сегмент выбирается по сроку сертификата: у Let's Encrypt границы 16-18 декабря и июня, а не начало полугодия.
func TestSegmentByNotAfter(t *testing.T) {
	list := excerpt(t)
	if len(list) != 11 {
		t.Fatalf("плиточных журналов %d, ждали 11", len(list))
	}
	const (
		sycamoreOld = "Let's Encrypt Sycamore2026h2"
		sycamore    = "Let's Encrypt Sycamore2027h1"
		willow      = "Let's Encrypt Willow2027h1"
	)
	cases := []struct {
		notAfter string
		want     []string
	}{
		{"2026-06-17T23:59:59Z", nil},
		{"2026-12-15T23:59:59Z", []string{sycamoreOld}},
		{"2026-12-16T00:00:00Z", []string{sycamoreOld, willow}},
		{"2026-12-16T23:59:59Z", []string{sycamoreOld, willow}},
		{"2026-12-17T00:00:00Z", []string{sycamore, willow}},
		{"2027-02-12T19:07:11Z", []string{sycamore, willow}},
		{"2027-06-17T00:00:00Z", []string{sycamore}},
		{"2027-06-18T00:00:00Z", nil},
	}
	for _, c := range cases {
		var got []string
		for _, l := range list {
			if l.Operator == letsEncrypt && l.accepts(date(t, c.notAfter)) {
				got = append(got, l.Name)
			}
		}
		if !slices.Equal(got, c.want) {
			t.Errorf("срок %s: %q, ждали %q", c.notAfter, got, c.want)
		}
	}
	// проверочный, только для наблюдения, ожидающий и отвергнутый журналы не читаются
	want := []string{
		"Google ParcelYard2027h1", sycamore, willow, "Geomys Tuscolo2027h1", "IPng Networks Gouda2027h1",
		"GoDaddy Aquamarine2027h1",
	}
	var got []string
	for _, l := range list {
		if l.accepts(date(t, "2027-02-12T19:07:11Z")) {
			got = append(got, l.Name)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("все операторы: %q, ждали %q", got, want)
	}
}

// Что читать: журналы Let's Encrypt - по номеру записи своей метки или по времени самой ранней метки,
// чужой журнал - только свою метку с номером и со сроком; окна одного журнала ближе секунды склеены.
func TestPlan(t *testing.T) {
	const (
		early = int64(1794686743184)
		prior = early - 86_400_000
	)
	notAfter := date(t, "2027-02-12T19:07:11Z")
	anchor := func(id string, ms, index int64, name string) Anchor {
		return Anchor{LogID: id, Time: time.UnixMilli(ms), Index: index, NotAfter: notAfter, Names: []string{name}}
	}
	anchors := []Anchor{
		anchor(sycamore2027h1, early+740, 1211694457, "a.example.com"),
		anchor(tiger2027h1, early, -1, "a.example.com"),
		// только классический журнал, выпущен через 600 мс после первого
		anchor(tiger2027h1, early+600, -1, "e.example.com"),
		// днём раньше: чужой плиточный журнал с номером, чужой без номера и неизвестный журнал
		anchor(tuscolo2027h1, prior, 500, "b.example.com"),
		anchor("Gouda43XkdHNBUnttgNV1ga2T60w23H+eI8Px8j7xLE=", prior-300, -1, "b.example.com"),
		anchor("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", prior-100, 7, "b.example.com"),
	}
	type planned struct {
		log     string
		at      int64
		index   int64
		limited bool
		self    string
	}
	want := []planned{
		{"Let's Encrypt Sycamore2027h1", early + 740, 1211694457, false, "a.example.com e.example.com"},
		{"Let's Encrypt Willow2027h1", early + 600, -1, false, "a.example.com e.example.com"},
		{"Geomys Tuscolo2027h1", prior, 500, true, "b.example.com"},
		{"Let's Encrypt Sycamore2027h1", prior - 300, -1, false, "b.example.com"},
		{"Let's Encrypt Willow2027h1", prior - 300, -1, false, "b.example.com"},
	}
	s := &search{web: newWeb(0, func() {}), list: excerpt(t), readers: map[string]*reader{}}
	var got []planned
	for _, one := range merge(s.plan(groupCerts(anchors))) {
		self := strings.Join(slices.Sorted(maps.Keys(one.self)), " ")
		got = append(got, planned{one.r.log.Name, one.at, one.index, one.limit > 0, self})
	}
	if !slices.Equal(got, want) {
		t.Errorf("окна:\n%+v\nждали:\n%+v", got, want)
	}
}

// Чужое имя домена в окне двигает край на полторы минуты от себя, имя самого сертификата - нет.
func TestWindowGrow(t *testing.T) {
	const at = int64(1795000000000)
	every := func(step, until int64) []hit {
		var out []hit
		for ms := step; ms <= until; ms += step {
			out = append(out, hit{name: "next.example.com", at: at + ms})
		}
		return out
	}
	cases := []struct {
		name     string
		hits     []hit
		from, to int64 // от метки, мс
	}{
		{"пусто", nil, -60_000, 60_000},
		{"имя самого сертификата", []hit{{"self.example.com", at + 40_000}}, -60_000, 60_000},
		{"чужое имя позже", []hit{{"new.example.com", at + 40_000}}, -60_000, 130_000},
		{"чужое имя раньше", []hit{{"new.example.com", at - 50_000}}, -140_000, 60_000},
		{"чужое имя на краю", []hit{{"new.example.com", at + 60_000}}, -60_000, 150_000},
		{"чужое имя за окном", []hit{{"new.example.com", at + 60_001}}, -60_000, 60_000},
		{"пачка тянется", every(50_000, 200_000), -60_000, 290_000},
		{"пачка длиннее предела", every(50_000, 900_000), -60_000, 600_000},
	}
	for _, c := range cases {
		w := &window{
			task: task{at: at, self: map[string]bool{"self.example.com": true}},
			got:  map[int64]tile{0: {hits: c.hits}},
			from: at - windowMs, to: at + windowMs,
		}
		for w.grow() {
		}
		if w.from-at != c.from || w.to-at != c.to {
			t.Errorf("%s: окно %d..%d, ждали %d..%d", c.name, w.from-at, w.to-at, c.from, c.to)
		}
	}
}
