package cmd

import (
	"fmt"
	"os"
	"sort"

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

func sortEntries(list []tui.Entry) []tui.Entry {
	sort.Slice(list, func(i, j int) bool { return list[i].Size > list[j].Size })
	return list
}
