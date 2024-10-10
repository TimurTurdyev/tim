// genman генерирует man-страницы tim; отдельная программа, чтобы генератор не попадал в сам бинарник.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra/doc"

	"github.com/TimurTurdyev/tim/cmd"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "использование: genman <папка>")
		os.Exit(2)
	}
	if err := os.MkdirAll(os.Args[1], 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	root := cmd.Root()
	header := &doc.GenManHeader{Title: "TIM", Section: "1", Source: "tim " + cmd.Version(), Manual: "tim"}
	if err := doc.GenManTree(root, header, os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
