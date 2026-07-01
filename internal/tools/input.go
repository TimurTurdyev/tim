package tools

import (
	"errors"
	"io"
	"os"
	"strings"
)

// words: аргументы, а без них слова из stdin, если он не терминал.
func words(args []string) ([]string, error) {
	text, err := textOrStdin(args)
	if err != nil {
		return nil, err
	}
	return strings.Fields(text), nil
}

// textOrStdin: аргументы одной строкой, а без них содержимое stdin, если он не терминал.
func textOrStdin(args []string) (string, error) {
	if len(args) > 0 {
		return strings.Join(args, " "), nil
	}
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice != 0 {
		return "", errors.New("нужен аргумент или ввод через stdin")
	}
	data, err := io.ReadAll(os.Stdin)
	return strings.TrimRight(string(data), "\n"), err
}
