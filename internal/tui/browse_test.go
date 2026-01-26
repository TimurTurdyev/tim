package tui

import "testing"

// Регрессия: отмеченные папка и её подпапка считались дважды, а вторая не убиралась - её уже нет.
func TestOutermost(t *testing.T) {
	got := Outermost([]Entry{{Path: "/d/backups"}, {Path: "/d/backups/waviot.ru"}, {Path: "/d/backups-old"}, {Path: "/d/demo"}})
	if len(got) != 3 || got[0].Path != "/d/backups" || got[1].Path != "/d/backups-old" {
		t.Errorf("%+v", got)
	}
}
