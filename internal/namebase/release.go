package namebase

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/TimurTurdyev/tim/internal/ui"
)

// GraphInfoURL: список выпусков, свежий первым.
const GraphInfoURL = "https://index.commoncrawl.org/graphinfo.json"

const (
	sourceRoot = "https://data.commoncrawl.org/"
	graphRoot  = "projects/hyperlinkgraph/"
	maxPaths   = 4 << 20
	maxID      = 100
)

// Release: выпуск списка хостов - его имя и сколько в нём имён.
type Release struct {
	ID    string
	Nodes int64
}

// ParseGraphInfo: самый свежий выпуск - в graphinfo.json он первый; хватает начала файла, где первая запись цела.
func ParseGraphInfo(data []byte) (Release, error) {
	var first struct {
		ID    string `json:"id"`
		Stats struct {
			Host struct {
				Nodes int64 `json:"nodes"`
			} `json:"host"`
		} `json:"stats"`
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') || !dec.More() {
		return Release{}, errors.New("список выпусков не разобран")
	}
	if err := dec.Decode(&first); err != nil {
		return Release{}, fmt.Errorf("список выпусков не разобран: %w", err)
	}
	rel := Release{ID: first.ID, Nodes: first.Stats.Host.Nodes}
	if !validID(rel.ID) || rel.Nodes <= 0 {
		return Release{}, fmt.Errorf("в списке выпусков нет имени или числа имён: %s", ui.Clip(ui.Plain(rel.ID), 80))
	}
	return rel, nil
}

// PathsURL: адрес списка частей выпуска.
func PathsURL(id string) string {
	return sourceRoot + graphRoot + id + "/host/" + id + "-host-vertices.paths.gz"
}

// ParsePaths: полные адреса частей выпуска в том порядке, в каком они идут в списке.
func ParsePaths(gz []byte) ([]string, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("список частей не читается: %w", err)
	}
	text, err := io.ReadAll(io.LimitReader(zr, maxPaths+1))
	if err != nil {
		return nil, fmt.Errorf("список частей не читается: %w", err)
	}
	if len(text) > maxPaths {
		return nil, errors.New("список частей слишком велик")
	}
	var urls []string
	for line := range strings.Lines(string(text)) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !validPath(line) {
			return nil, fmt.Errorf("в списке частей чужая строка: %s", ui.Clip(ui.Plain(line), 80))
		}
		urls = append(urls, sourceRoot+line)
	}
	if len(urls) == 0 {
		return nil, errors.New("список частей пуст")
	}
	return urls, nil
}

// validID: имя выпуска идёт в адрес, поэтому в нём только строчные буквы, цифры и дефис.
func validID(id string) bool {
	if id == "" || len(id) > maxID {
		return false
	}
	return !strings.ContainsFunc(id, func(r rune) bool {
		return r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9')
	})
}

func validPath(path string) bool {
	if !strings.HasPrefix(path, graphRoot) || !strings.HasSuffix(path, ".gz") || strings.Contains(path, "..") {
		return false
	}
	return !strings.ContainsFunc(path, func(r rune) bool {
		letter := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		return !letter && !strings.ContainsRune("/._-", r)
	})
}
