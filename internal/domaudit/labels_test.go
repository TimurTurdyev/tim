package domaudit

import (
	"slices"
	"testing"
)

// Имя подписывает самый надёжный из тех, кто его знает, в каком бы порядке они ни ответили.
func TestBest(t *testing.T) {
	cases := []struct {
		labels []string
		want   string
	}{
		{[]string{sourceTHC, fromWords, fromSiteCert}, fromSiteCert},
		{[]string{fromWords, fromBase}, fromBase},
		{[]string{fromPattern, sourceHackertarget}, sourceHackertarget},
		{[]string{fromCert, fromLog}, fromLog},
		{[]string{"новый источник", fromPattern}, fromPattern},
		{nil, ""},
	}
	for _, c := range cases {
		if got := best(c.labels); got != c.want {
			t.Errorf("%v: %q, ждали %q", c.labels, got, c.want)
		}
	}
}

// Итог: имя, которое знает хоть один свой источник, найдено самими; сторонние добавили то, чего свои не знают.
func TestFound(t *testing.T) {
	c := newCollector("example.com", Options{})
	c.asking = &asking{
		list: []source{{name: sourceTHC, label: sourceTHC}, {name: "crt.sh", label: fromCert}},
		answers: []*sourceAnswer{
			{got: nameList{Subs: []string{"api.example.com", "old.example.com"}}, state: SourceState{Name: sourceTHC, Names: 2}},
			{state: SourceState{Name: "crt.sh", Error: errSlow.Error()}},
		},
	}
	c.learn("api.example.com", fromWords)
	c.learn("api.example.com", fromPage)
	c.learn("mail.example.com", fromSPF)
	c.learn("gone.example.com", fromWords)
	c.learn("example.net", fromPage)
	for _, name := range []string{"example.com", "api.example.com", "old.example.com", "mail.example.com"} {
		c.hosts[name] = &Host{Name: name, IPs: []string{"192.0.2.1"}}
	}
	c.finish()
	r := c.rep
	if r.Found.Own != 3 || r.Found.Added != 1 || !slices.Equal(r.Found.Silent, []string{"crt.sh"}) {
		t.Errorf("итог: %+v", r.Found)
	}
	want := map[string]struct {
		source  string
		sources []string
	}{
		"example.com":      {fromApex, nil},
		"api.example.com":  {fromPage, []string{fromWords, fromPage, sourceTHC}},
		"old.example.com":  {sourceTHC, []string{sourceTHC}},
		"mail.example.com": {fromSPF, []string{fromSPF}},
	}
	for _, h := range r.Hosts {
		if w := want[h.Name]; h.Source != w.source || !slices.Equal(h.Sources, w.sources) {
			t.Errorf("%s: подпись %q, знают %v; ждали %q и %v", h.Name, h.Source, h.Sources, w.source, w.sources)
		}
	}
	// Свои источники первыми; словарь назвал два имени, хотя живое одно.
	var names []string
	for _, s := range r.Sources {
		names = append(names, s.Name)
		if s.Name == fromWords && (s.Names != 2 || !s.Own) {
			t.Errorf("словарь: %+v", s)
		}
	}
	if !slices.Equal(names, []string{fromWords, fromSPF, fromPage, sourceTHC, "crt.sh"}) {
		t.Errorf("источники: %v", names)
	}
}
