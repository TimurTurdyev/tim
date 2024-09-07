package runner

import (
	"bufio"
	"os"
	"strings"
)

var stdin = bufio.NewReader(os.Stdin)

func fmtScan(dst *string) (int, error) {
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		return 0, err
	}
	*dst = strings.TrimSpace(line)
	return 1, nil
}
