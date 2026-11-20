package namebase

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/ui"
)

const segSuffix = ".seg"

// Update ставит выпуск rel часть за частью без второй копии архива: open(i) отдаёт часть i (gzip) и её ETag; rel.Nodes 0 - число имён не сверять.
func Update(dir string, rel Release, parts int, open func(i int) (io.ReadCloser, string, error), progress func(done, total int)) error {
	return update(dir, blockSize, rel, parts, open, progress)
}

// Pending: с какой части продолжать установку выпуска rel; 0 - с начала, число частей - выпуск уже стоит.
func Pending(dir string, rel Release) int {
	m, err := loadManifest(dir)
	if err != nil || m.Release != rel.ID {
		return 0
	}
	return len(m.Segments)
}

func update(dir string, block int, rel Release, parts int, open func(i int) (io.ReadCloser, string, error), progress func(done, total int)) error {
	if !validID(rel.ID) || parts <= 0 {
		return fmt.Errorf("выпуск не годится: %s, частей %d", ui.Clip(ui.Plain(rel.ID), 80), parts)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("папка архива имён: %w", err)
	}
	m, err := loadManifest(dir)
	if errors.Is(err, ErrNoBase) {
		m, err = &manifest{}, nil
	}
	if err != nil {
		return err
	}
	if err := m.begin(rel.ID, parts); err != nil {
		return err
	}
	m.sweep(dir)
	for i := len(m.Segments); i < parts; i++ {
		if err := m.addPart(dir, i, block, open); err != nil {
			return fmt.Errorf("часть %d из %d: %w", i+1, parts, err)
		}
		if progress != nil {
			progress(i+1, parts)
		}
	}
	if err := m.finish(dir); err != nil {
		return err
	}
	return m.checkTotal(rel.Nodes)
}

// begin: тот же выпуск продолжается; при другом всё, что стоит, становится прежним архивом.
func (m *manifest) begin(release string, parts int) error {
	if m.Release == release {
		if len(m.Segments) > parts {
			return fmt.Errorf("в архиве %d частей выпуска, а в выпуске их %d", len(m.Segments), parts)
		}
		m.Parts = parts
		return nil
	}
	var old []segment
	for _, s := range m.Segments {
		if s.Names > 0 {
			old = append(old, s)
		}
	}
	for _, s := range m.Old {
		s.After = max(s.After, m.Cut)
		old = append(old, s)
	}
	*m = manifest{Release: release, Parts: parts, Gen: m.Gen + 1, Updated: m.Updated, Old: old}
	return nil
}

// sweep убирает из папки следы прерванного обновления: временные файлы и сегменты, которых нет в описании.
func (m *manifest) sweep(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	known := func(name string) bool {
		return slices.ContainsFunc(slices.Concat(m.Segments, m.Old), func(s segment) bool { return s.File == name })
	}
	for _, e := range entries {
		name := e.Name()
		draft := strings.HasPrefix(name, tmpPrefix) && strings.HasSuffix(name, tmpSuffix)
		orphan := strings.HasSuffix(name, segSuffix) && !known(name)
		if (draft || orphan) && !e.IsDir() {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}

// addPart собирает сегмент части i, ставит его в описание и убирает прежние сегменты, которые он перекрыл.
func (m *manifest) addPart(dir string, i, block int, open func(i int) (io.ReadCloser, string, error)) error {
	src, etag, err := open(i)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	path := filepath.Join(dir, fmt.Sprintf("%04d-%05d%s", m.Gen, i, segSuffix))
	seg, err := buildSegment(path, src, etag, m.Cut, block)
	if err != nil {
		return err
	}
	m.Segments = append(m.Segments, seg)
	if seg.Names > 0 {
		m.Cut = seg.Last
	}
	var gone []string
	m.Old = slices.DeleteFunc(m.Old, func(s segment) bool {
		covered := s.Last <= max(s.After, m.Cut)
		if covered {
			gone = append(gone, s.File)
		}
		return covered
	})
	if err := m.commit(dir, gone); err != nil {
		if seg.File != "" {
			_ = os.Remove(path)
		}
		return err
	}
	return nil
}

// finish: все части на месте, остаток прежнего архива больше не нужен.
func (m *manifest) finish(dir string) error {
	if len(m.Old) == 0 {
		return nil
	}
	var gone []string
	for _, s := range m.Old {
		gone = append(gone, s.File)
	}
	m.Old = nil
	return m.commit(dir, gone)
}

// commit пишет описание и только потом удаляет файлы, которые из него ушли.
func (m *manifest) commit(dir string, gone []string) error {
	m.Updated = time.Now().UTC().Truncate(time.Second)
	if err := m.save(dir); err != nil {
		return fmt.Errorf("описание архива имён не записалось: %w", err)
	}
	for _, file := range gone {
		_ = os.Remove(filepath.Join(dir, file))
	}
	return nil
}

// checkTotal: в частях должно быть столько строк, сколько имён обещал выпуск.
func (m *manifest) checkTotal(nodes int64) error {
	var lines int64
	for _, s := range m.Segments {
		lines += s.Source
	}
	if nodes > 0 && lines != nodes {
		return fmt.Errorf("в частях %d имён, а выпуск обещал %d", lines, nodes)
	}
	return nil
}
