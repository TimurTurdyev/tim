// Package namebase: имена поддоменов без сети - встроенный словарь меток и локальный архив имён из списка хостов Common Crawl.
package namebase

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"strings"
	"sync"
)

// WordsVersion: из какого выпуска списка хостов собран встроенный словарь.
const WordsVersion = "2026-10"

//go:embed words.txt.gz
var wordsPacked []byte

var words = sync.OnceValue(unpackWords)

// Words: первые n меток словаря по убыванию частоты; n больше длины - весь список. Срез общий, не менять.
func Words(n int) []string {
	all := words()
	n = max(0, min(n, len(all)))
	return all[:n:n]
}

func unpackWords() []string {
	zr, err := gzip.NewReader(bytes.NewReader(wordsPacked))
	if err != nil {
		return nil
	}
	text, err := io.ReadAll(zr)
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimRight(string(text), "\n"), "\n")
}
