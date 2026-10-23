package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/TimurTurdyev/tim/internal/diskuse"
	"github.com/TimurTurdyev/tim/internal/tui"
)

var diskBrowse bool

// diskBrowseRun: обозреватель по папкам. Корень - весь занятый диск: домашняя папка, программы,
// тома и то, что не разобрать, так что сумма сходится с df и видно, куда копать.
func diskBrowseRun(args []string) error {
	st, err := newDiskState(args)
	if err != nil {
		return err
	}
	// Замер до экрана: ход печатается в stderr, пока терминал ещё не занят.
	st.ensure(false, cliLine)
	fmt.Fprint(os.Stderr, "\r\033[2K")
	return runScreen(browseScreen(st))
}

// moveInTree: папка уехала в Корзину - её размер уходит от прежних родителей и приходит к Корзине.
// На диске место не освободилось, пока Корзина не очищена, и обозреватель должен это показывать.
func moveInTree(tree diskuse.Tree, path, trash string) {
	size := tree[path]
	for k := range tree {
		if k == path || strings.HasPrefix(k, path+"/") {
			delete(tree, k)
		}
	}
	for d := filepath.Dir(path); d != "/" && d != "."; d = filepath.Dir(d) {
		if _, ok := tree[d]; ok {
			tree[d] -= size
		}
	}
	tree[trash] += size
	for d := filepath.Dir(trash); d != "/" && d != "."; d = filepath.Dir(d) {
		if _, ok := tree[d]; ok {
			tree[d] += size
		}
	}
}

func sortEntries(list []tui.Entry) []tui.Entry {
	sort.Slice(list, func(i, j int) bool { return list[i].Size > list[j].Size })
	return list
}
