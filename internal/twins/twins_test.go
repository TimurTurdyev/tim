package twins

import "testing"

func TestCandidates(t *testing.T) {
	c := Candidates("lpack-spb.ru")
	want := map[string]string{
		"lpak-spb.ru":   "опечатка",      // пропуск
		"lpackk-spb.ru": "опечатка",      // повтор
		"plack-spb.ru":  "опечатка",      // перестановка
		"lpaxk-spb.ru":  "опечатка",      // соседняя клавиша
		"1pack-spb.ru":  "похожие буквы", // l -> 1
		"lpackspb.ru":   "дефис",
		"lpack-spb.com": "другая зона",
	}
	for name, how := range want {
		if c[name] != how {
			t.Errorf("%s: %q, ожидали %q", name, c[name], how)
		}
	}
	if _, ok := c["lpack-spb.ru"]; ok {
		t.Error("сам домен попал в двойники")
	}
	if _, ok := c["-pack-spb.ru"]; ok {
		t.Error("метка с дефисом в начале")
	}
}
