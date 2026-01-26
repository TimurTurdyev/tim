package cmd

import (
	"testing"

	"github.com/TimurTurdyev/tim/internal/diskuse"
)

// Убранное в Корзину место не исчезает с диска: оно переезжает к Корзине, родители худеют.
func TestMoveInTree(t *testing.T) {
	tree := diskuse.Tree{"/h": 100, "/h/Dev": 60, "/h/Dev/data": 50, "/h/Dev/data/backups": 30, "/h/Dev/data/backups/x": 20}
	moveInTree(tree, "/h/Dev/data/backups", "/h/.Trash")
	want := diskuse.Tree{"/h": 100, "/h/Dev": 30, "/h/Dev/data": 20, "/h/.Trash": 30}
	if len(tree) != len(want) {
		t.Fatalf("%v", tree)
	}
	for k, v := range want {
		if tree[k] != v {
			t.Errorf("%s: %d, want %d", k, tree[k], v)
		}
	}
}
