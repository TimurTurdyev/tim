package leftover

import "testing"

func TestJudge(t *testing.T) {
	ix := NewIndex([]App{
		{ID: "com.tdesktop.Telegram", Name: "Telegram"},
		{ID: "com.google.Chrome", Name: "Google Chrome"},
		{ID: "com.docker.docker", Name: "Docker"},
	})
	cases := []struct {
		name          string
		orphan, sure  bool
		owner, simile string
	}{
		{"com.valvesoftware.steam.helper.plist", true, true, "com.valvesoftware", ""},
		{"6N38VWS5BX.ru.keepcoder.Telegram", true, true, "ru.keepcoder", "Telegram"},
		{"com.google.Chrome.helper", false, false, "com.google", ""},
		{"com.apple.Safari.savedState", false, false, "com.apple", ""},
		{"uk.co.hellogames.nms", true, true, "uk.co.hellogames", ""},
		{"Google", false, false, "Google", ""},
		{"Docker Desktop", false, false, "Docker Desktop", ""},
		{"CloudDocs", false, false, "CloudDocs", ""},
		{"Steam", true, false, "Steam", ""},
	}
	for _, c := range cases {
		v := ix.Judge(c.name)
		if v.Orphan != c.orphan || v.Sure != c.sure || v.Owner != c.owner || v.Similar != c.simile {
			t.Errorf("%s: %+v", c.name, v)
		}
	}
}
