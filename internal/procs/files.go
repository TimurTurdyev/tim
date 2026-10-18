package procs

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Outputs: в какие файлы процесс пишет вывод и ошибки. Терминал, канал и /dev/null - не файлы, пусто.
func Outputs(pid string) (stdout, stderr string) {
	if _, err := strconv.Atoi(pid); err != nil {
		return "", ""
	}
	out, err := runner.Output("lsof", "-a", "-p", pid, "-d", "1,2", "-Fftn")
	if err != nil && out == "" {
		return "", ""
	}
	return ParseOutputs(out)
}

// ParseOutputs разбирает lsof -Fftn: f<номер>, t<тип>, n<имя>; обычный файл - тип REG.
func ParseOutputs(out string) (stdout, stderr string) {
	fd, kind := "", ""
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'f':
			fd, kind = line[1:], ""
		case 't':
			kind = line[1:]
		case 'n':
			if kind != "REG" {
				continue
			}
			switch fd {
			case "1":
				stdout = line[1:]
			case "2":
				stderr = line[1:]
			}
		}
	}
	return stdout, stderr
}
