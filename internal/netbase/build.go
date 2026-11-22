package netbase

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

const (
	risFile4     = "riswhoisdump.IPv4.gz"
	risFile6     = "riswhoisdump.IPv6.gz"
	namesFile    = "asn.txt"
	countryFile4 = "server-country-ipv4.csv"
	countryFile6 = "server-country-ipv6.csv"

	risURL     = "https://www.ris.ripe.net/dumps/"
	namesURL   = "https://ftp.ripe.net/ripe/asnames/"
	releaseURL = "https://github.com/sapics/ip-location-db/releases/download/"
	mirrorURL  = "https://cdn.jsdelivr.net/gh/sapics/ip-location-db@main/server-country/"

	brokenLimit = 100 // одна битая строка на столько строк источника - ещё терпимо
)

// Sources: что скачать для Build. У дампов RIS и имён AS контрольной суммы нет, их проверяет разбор.
var Sources = []Source{
	{File: risFile4, URLs: []string{risURL + risFile4}},
	{File: risFile6, URLs: []string{risURL + risFile6}},
	{File: namesFile, URLs: []string{namesURL + namesFile}},
	{File: countryFile4, URLs: []string{releaseURL + "latest/" + countryFile4, mirrorURL + countryFile4},
		SHA256URL: releaseURL + "checksum/" + countryFile4 + ".sha256"},
	{File: countryFile6, URLs: []string{releaseURL + "latest/" + countryFile6, mirrorURL + countryFile6},
		SHA256URL: releaseURL + "checksum/" + countryFile6 + ".sha256"},
}

// Source: файл для сборки. URLs - основной адрес и запасные, File - под каким именем его ждёт Build.
type Source struct {
	File      string
	URLs      []string
	SHA256URL string
}

// Stats: сколько сетей, имён AS и диапазонов стран дошло до таблиц и сколько таблицы занимают.
type Stats struct {
	Nets4, Nets6, Names, Countries4, Countries6 int
	Bytes                                       int64
}

// section: содержимое одной таблицы и счёт строк её источника.
type section struct {
	keys, vals, text    []byte
	lines, broken, rows int
}

// Build собирает таблицы в dir из файлов каталога from; при любой ошибке прежние таблицы остаются как были.
func Build(from, dir string) (Stats, error) {
	return build(from, dir, time.Now())
}

func build(from, dir string, now time.Time) (Stats, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Stats{}, fmt.Errorf("каталог таблиц: %w", err)
	}
	var temps [tableCount]string
	defer func() {
		for _, tmp := range temps {
			if tmp != "" {
				_ = os.Remove(tmp)
			}
		}
	}()
	var rows [tableCount]int
	var size int64
	for i, spec := range specs {
		sec, err := parseSource(filepath.Join(from, spec.source), spec)
		if err == nil {
			err = accept(sec, rowsBefore(filepath.Join(dir, spec.file), spec))
		}
		if err != nil {
			return Stats{}, fmt.Errorf("источник %s: %w", spec.source, err)
		}
		tmp, written, err := writeTemp(dir, spec, sec, now)
		temps[i] = tmp
		if err != nil {
			return Stats{}, fmt.Errorf("запись таблицы %s: %w", spec.file, err)
		}
		rows[i] = sec.rows
		size += written
	}
	for i, spec := range specs {
		if err := os.Rename(temps[i], filepath.Join(dir, spec.file)); err != nil {
			return Stats{}, fmt.Errorf("замена таблицы %s: %w", spec.file, err)
		}
	}
	return Stats{Nets4: rows[as4], Nets6: rows[as6], Names: rows[names], Countries4: rows[cc4],
		Countries6: rows[cc6], Bytes: size}, nil
}

// accept: источник годен, если битых строк немного и годных не меньше 80% от прежней таблицы.
func accept(sec section, before int) error {
	switch {
	case sec.broken*brokenLimit > sec.lines:
		return fmt.Errorf("битых строк %d из %d", sec.broken, sec.lines)
	case sec.rows == 0:
		return errors.New("нет ни одной годной строки")
	case sec.rows*5 < before*4:
		return fmt.Errorf("годных строк %d, в прежней таблице было %d", sec.rows, before)
	}
	return nil
}

func parseSource(path string, spec tableSpec) (section, error) {
	f, err := os.Open(path)
	if err != nil {
		return section{}, err
	}
	defer func() { _ = f.Close() }()
	text, err := plain(f)
	if err != nil {
		return section{}, err
	}
	switch spec.kind {
	case kindAS:
		return parseRIS(text, spec.keyLen)
	case kindNames:
		return parseNames(text)
	}
	return parseCountry(text, spec.keyLen)
}

// plain: текст файла; gzip узнаётся по первым байтам, а не по имени - зеркала отдают и так, и так.
func plain(f io.Reader) (io.Reader, error) {
	buf := bufio.NewReader(f)
	head, _ := buf.Peek(2)
	if len(head) < 2 || head[0] != 0x1f || head[1] != 0x8b {
		return buf, nil
	}
	unpacked, err := gzip.NewReader(buf)
	if err != nil {
		return nil, err
	}
	return unpacked, nil
}
