package scan

import "testing"

func TestNamesWrapperAndClashes(t *testing.T) {
	clones := []string{
		"/r/projects/shop/app",
		"/r/clients/shop/app",
		"/r/projects/blog",
		"/r/work/Efm",
	}
	got := Names(clones, "app", nil)
	want := []string{"projects-shop", "clients-shop", "blog", "efm"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %q want %q", i, got[i], want[i])
		}
	}
}

// Одна обёртка shop/app не даёт угадать wrapper по повторам - имя всё равно shop, а не app.
func TestNamesPlainWithoutWrapper(t *testing.T) {
	got := Names([]string{"/r/Dev/shop/app", "/r/Dev/blog"}, "", nil)
	if got[0] != "shop" || got[1] != "blog" {
		t.Errorf("got %q", got)
	}
}

func TestNamesAvoidTaken(t *testing.T) {
	got := Names([]string{"/r/clients/blog"}, "", map[string]bool{"blog": true})
	if got[0] != "clients-blog" {
		t.Errorf("got %q", got[0])
	}
}
