// Package release: последний релиз tim на GitHub, выбор архива, проверка суммы, распаковка.
package release

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"io"
	"strconv"
	"strings"
)

type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
}

type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// PickAsset: архив под систему; universal-сборка для darwin подходит любой архитектуре.
func PickAsset(assets []Asset, goos, goarch string) (Asset, bool) {
	var fallback Asset
	found := false
	for _, a := range assets {
		n := strings.ToLower(a.Name)
		if !strings.HasSuffix(n, ".tar.gz") || !strings.Contains(n, goos) {
			continue
		}
		if strings.Contains(n, "_"+goarch+".") || strings.Contains(n, "_"+goarch+"_") {
			return a, true
		}
		if strings.Contains(n, "_all.") {
			fallback, found = a, true
		}
	}
	return fallback, found
}

// ParseChecksums: файл вида "<sha256>  <имя>" в карту имя -> сумма.
func ParseChecksums(r io.Reader) map[string]string {
	m := map[string]string{}
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && len(f[0]) == 64 {
			m[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
		}
	}
	return m
}

// Newer: тег latest новее current; dev и пустая версия считаются старее любой.
func Newer(latest, current string) bool {
	l, c := nums(latest), nums(current)
	if len(c) == 0 {
		return len(l) > 0
	}
	for i := range max(len(l), len(c)) {
		var x, y int
		if i < len(l) {
			x = l[i]
		}
		if i < len(c) {
			y = c[i]
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func nums(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "-")
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out
		}
		out = append(out, n)
	}
	return out
}

// ExtractTar достаёт файл name из tar.gz.
func ExtractTar(r io.Reader, name string) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errors.New("в архиве нет " + name)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && (h.Name == name || strings.HasSuffix(h.Name, "/"+name)) {
			return io.ReadAll(io.LimitReader(tr, 200<<20))
		}
	}
}
