package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/config"
	"github.com/TimurTurdyev/tim/internal/fetch"
	"github.com/TimurTurdyev/tim/internal/namebase"
	"github.com/TimurTurdyev/tim/internal/netbase"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/report"
)

const (
	commonCrawl = "https://data.commoncrawl.org/"
	listLimit   = 4 << 20
	headLimit   = 8 << 10 // начала списка выпусков хватает, а без VPN целиком он и не приходит
	tableLimit  = 256 << 20
	partLimit   = 1 << 30
	netsFresh   = 30 * 24 * time.Hour
	// Сколько байт на имя занимает выпуск: в загрузке и на диске после упаковки.
	bytesToFetch = 7.7
	bytesOnDisk  = 4.0
	progressStep = 200 * time.Millisecond
)

// dataOptions: что обновлять и откуда брать список хостов.
type dataOptions struct {
	nets, names bool
	from        string
}

// dataState: что из локальной базы стоит на машине.
type dataState struct {
	Dir   string      `json:"dir"`
	Nets  *netsState  `json:"nets,omitempty"`
	Names *namesState `json:"names,omitempty"`
}

type netsState struct {
	Built time.Time `json:"built"`
	Bytes int64     `json:"bytes"`
}

// namesState: Ready из Parts - сколько частей выпуска стоит; меньше - обновление не закончено.
type namesState struct {
	Release string    `json:"release"`
	Names   int64     `json:"names"`
	Updated time.Time `json:"updated"`
	Bytes   int64     `json:"bytes"`
	Ready   int       `json:"ready"`
	Parts   int       `json:"parts"`
}

// removing: скачанная часть нужна, пока из неё собирают сегмент.
type removing struct {
	*os.File
}

func (r removing) Close() error {
	err := r.File.Close()
	_ = os.Remove(r.Name())
	return err
}

// dataStatus: что стоит и какого возраста.
func dataStatus() error {
	st := readData()
	facts := report.Facts{{Key: "каталог", Value: paths.Show(st.Dir)}}
	switch n := st.Nets; {
	case n == nil:
		facts = append(facts, report.Fact{Key: "сети и страны", Value: "нет - tim data update (25 МБ, качается и без VPN)", Level: report.Info})
	case time.Since(n.Built) > netsFresh:
		facts = append(facts, report.Fact{Key: "сети и страны", Level: report.Warn,
			Value: "от " + ui.DateLong(n.Built) + ", " + ui.Bytes(n.Bytes) + " - старше 30 дней, вместо них спрашивается сеть: tim data update"})
	default:
		facts = append(facts, report.Fact{Key: "сети и страны", Value: "от " + ui.DateLong(n.Built) + ", " + ui.Bytes(n.Bytes)})
	}
	switch n := st.Names; {
	case n == nil:
		facts = append(facts, report.Fact{Key: "архив имён", Level: report.Info,
			Value: "нет - tim data update --names (скачать около 1.9 ГБ, на диске 1 ГБ; нужен VPN или зеркало)"})
	case n.Ready < n.Parts:
		facts = append(facts, report.Fact{Key: "архив имён", Level: report.Warn,
			Value: fmt.Sprintf("выпуск %s ставится: %d из %d частей - продолжить: tim data update --names", n.Release, n.Ready, n.Parts)})
	default:
		facts = append(facts, report.Fact{Key: "архив имён",
			Value: fmt.Sprintf("выпуск %s, имён %s, %s, от %s", n.Release, grouped(n.Names), ui.Bytes(n.Bytes), ui.DateLong(n.Updated))})
	}
	return view.Show(report.Report{Schema: "tim.data/1", Title: "Локальная база", Data: st, Blocks: []report.Block{facts,
		report.Note("из неё tim domain и tim ip берут сети, страны и имена без сторонних сервисов")}}, view.AsJSON)
}

func readData() dataState {
	st := dataState{Dir: paths.Data()}
	if t, err := netbase.Open(paths.Data("nets")); err == nil {
		st.Nets = &netsState{Built: t.Built(), Bytes: dirSize(paths.Data("nets"))}
	}
	if b, err := namebase.Open(paths.Data("names")); err == nil {
		ready, parts := b.Parts()
		st.Names = &namesState{Release: b.Release(), Names: b.Names(), Updated: b.Updated(), Bytes: dirSize(paths.Data("names")), Ready: ready, Parts: parts}
	}
	return st
}

// dataUpdate: таблицы сетей - всегда, архив имён - если просили или он уже стоит.
func dataUpdate(cfg *config.Config, o dataOptions) error {
	nets, names := o.nets, o.names || o.from != ""
	if !nets && !names {
		_, err := namebase.Open(paths.Data("names"))
		nets, names = true, err == nil
	}
	if nets {
		if err := updateNets(); err != nil {
			return err
		}
	}
	if names {
		return updateNames(cfg, o.from)
	}
	if !o.nets {
		ui.Println(ui.Dim("архив имён не стоит: tim data update --names (скачать около 1.9 ГБ, нужен VPN или зеркало)"))
	}
	return nil
}

// updateNets: дампы RIS, имена AS и страны серверов - в таблицы; прежние остаются, пока новые не собраны.
func updateNets() error {
	dir := paths.Data("nets")
	tmp := filepath.Join(dir, "download")
	if runner.DryRun {
		for _, src := range netbase.Sources {
			ui.Println(ui.Dim("$ скачал бы " + src.URLs[0]))
		}
		return nil
	}
	for _, src := range netbase.Sources {
		if err := fetchSource(src, filepath.Join(tmp, src.File)); err != nil {
			return ui.Errorf("%s не скачан: %v", src.File, err)
		}
	}
	stats, err := netbase.Build(tmp, dir)
	if err != nil {
		return ui.Errorf("таблицы сетей не собраны, прежние на месте: %v", err)
	}
	_ = os.RemoveAll(tmp)
	ui.Println(ui.Label("сети и страны ") + fmt.Sprintf("сетей %s и %s (IPv6), имён AS %s, %s", grouped(int64(stats.Nets4)), grouped(int64(stats.Nets6)),
		grouped(int64(stats.Names)), ui.Bytes(stats.Bytes)))
	return nil
}

// fetchSource: файл с основного адреса, не вышло - с запасного; сумма сверяется, если источник её публикует.
func fetchSource(src netbase.Source, dst string) error {
	opt := fetch.Options{MaxBytes: tableLimit, Progress: progress(src.File)}
	if src.SHA256URL != "" {
		if sum, _, err := fetch.Bytes(context.Background(), src.SHA256URL, listLimit, nil); err == nil {
			opt.SHA256, _, _ = strings.Cut(strings.TrimSpace(string(sum)), " ")
		}
	}
	var last error
	for _, url := range src.URLs {
		if _, last = fetch.File(context.Background(), url, dst, opt); last == nil {
			return nil
		}
	}
	return last
}

// updateNames: свежий выпуск списка хостов часть за частью; прерванное продолжается с той же части.
func updateNames(cfg *config.Config, from string) error {
	dir := paths.Data("names")
	rel, urls, err := latestRelease(cfg.Data.Mirror)
	if from != "" {
		return namesFromDir(dir, from, rel, urls)
	}
	if err != nil {
		return ui.Errorf("свежий выпуск списка хостов не узнать: %v", err)
	}
	if base, err := namebase.Open(dir); err == nil && base.Release() == rel.ID {
		if ready, parts := base.Parts(); ready == parts {
			ui.Println(ui.Label("архив имён ") + "свежий: выпуск " + rel.ID)
			return nil
		}
	}
	ui.Println(ui.Dim(fmt.Sprintf("выпуск %s: имён %s, частей %d, скачать около %s, на диске около %s", rel.ID, grouped(rel.Nodes), len(urls),
		ui.Bytes(int64(float64(rel.Nodes)*bytesToFetch)), ui.Bytes(int64(float64(rel.Nodes)*bytesOnDisk)))))
	if runner.DryRun {
		ui.Println(ui.Dim("$ скачал бы " + urls[0] + " и ещё " + grouped(int64(len(urls)-1))))
		return nil
	}
	open := func(i int) (io.ReadCloser, string, error) {
		dst := filepath.Join(dir, "download", fmt.Sprintf("part-%05d.gz", i))
		info, err := fetch.File(context.Background(), urls[i], dst, fetch.Options{MaxBytes: partLimit, Progress: progress(fmt.Sprintf("часть %d из %d", i+1, len(urls)))})
		if err != nil {
			return nil, "", err
		}
		f, err := os.Open(dst)
		return removing{f}, info.ETag, err
	}
	err = namebase.Update(dir, rel, len(urls), open, nil)
	if errors.Is(err, fetch.ErrStalled) {
		return ui.Errorf("данные не идут: Common Crawl без VPN отдаёт первые килобайты и замирает. Нужен VPN или зеркало ([data] mirror в конфиге). " +
			"Скачанное сохранено, продолжить: tim data update --names")
	}
	if err != nil {
		return ui.Errorf("архив имён не обновлён: %v", err)
	}
	ui.Println(ui.Label("архив имён ") + "выпуск " + rel.ID + ", имён " + grouped(rel.Nodes))
	return nil
}

// latestRelease: свежий выпуск и адреса его частей; mirror - откуда качать вместо data.commoncrawl.org.
func latestRelease(mirror string) (namebase.Release, []string, error) {
	head, _, err := fetch.Bytes(context.Background(), namebase.GraphInfoURL, headLimit, http.Header{"Range": {fmt.Sprintf("bytes=0-%d", headLimit-1)}})
	if err != nil {
		return namebase.Release{}, nil, err
	}
	rel, err := namebase.ParseGraphInfo(head)
	if err != nil {
		return namebase.Release{}, nil, err
	}
	list, _, err := fetch.Bytes(context.Background(), mirrored(namebase.PathsURL(rel.ID), mirror), listLimit, nil)
	if err != nil {
		return namebase.Release{}, nil, err
	}
	urls, err := namebase.ParsePaths(list)
	for i := range urls {
		urls[i] = mirrored(urls[i], mirror)
	}
	return rel, urls, err
}

// mirrored: тот же путь на зеркале.
func mirrored(url, mirror string) string {
	if mirror == "" {
		return url
	}
	return strings.TrimSuffix(mirror, "/") + "/" + strings.TrimPrefix(url, commonCrawl)
}

// namesFromDir: архив из уже скачанных частей. Если это части свежего выпуска, он так и называется; иначе имя
// берётся из самих файлов, а число имён не сверяется.
func namesFromDir(dir, from string, rel namebase.Release, urls []string) error {
	files, _ := filepath.Glob(filepath.Join(from, "part-*.gz"))
	slices.Sort(files)
	if len(files) == 0 {
		return ui.Errorf("в %s нет частей списка хостов: ждал файлы part-*.gz", from)
	}
	same := len(files) == len(urls)
	for i := range urls {
		same = same && filepath.Base(files[i]) == filepath.Base(urls[i])
	}
	if !same {
		rel = namebase.Release{ID: localRelease(files[0])}
	}
	if runner.DryRun {
		ui.Println(ui.Dim(fmt.Sprintf("$ собрал бы архив имён из %d частей в %s", len(files), paths.Show(dir))))
		return nil
	}
	open := func(i int) (io.ReadCloser, string, error) {
		f, err := os.Open(files[i])
		return f, "", err
	}
	if err := namebase.Update(dir, rel, len(files), open, partsDone()); err != nil {
		return ui.Errorf("архив имён не собран: %v", err)
	}
	ui.Println(ui.Label("архив имён ") + "выпуск " + rel.ID + ", частей " + grouped(int64(len(files))))
	return nil
}

// localRelease: имя выпуска для частей из папки - по метке в имени файла "part-00000-<метка>-c000.txt.gz".
func localRelease(file string) string {
	mark := strings.TrimPrefix(filepath.Base(file), "part-00000-")
	mark, _, _ = strings.Cut(mark, "-")
	id := "local-" + strings.ToLower(mark)
	if strings.ContainsFunc(id, func(r rune) bool { return r != '-' && (r < 'a' || r > 'z') && (r < '0' || r > '9') }) {
		return "local"
	}
	return id
}

// progress: ход загрузки строкой в stderr, пока команда идёт в терминале.
func progress(what string) func(got, total int64) {
	if !ui.Terminal() {
		return nil
	}
	var shown time.Time
	return func(got, total int64) {
		if done := total > 0 && got >= total; time.Since(shown) < progressStep && !done {
			return
		}
		shown = time.Now()
		text := what + ": " + ui.Bytes(got)
		if total > 0 {
			text += " из " + ui.Bytes(total)
		}
		fmt.Fprint(os.Stderr, "\r\033[2K"+ui.Dim(text))
		if total > 0 && got >= total {
			fmt.Fprint(os.Stderr, "\r\033[2K")
		}
	}
}

// partsDone: ход сборки архива из готовых частей.
func partsDone() func(done, total int) {
	if !ui.Terminal() {
		return nil
	}
	return func(done, total int) {
		fmt.Fprintf(os.Stderr, "\r\033[2K"+ui.Dim("архив имён: часть %d из %d"), done, total)
		if done == total {
			fmt.Fprint(os.Stderr, "\r\033[2K")
		}
	}
}

func dirSize(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// grouped: число с пробелами между тысячами.
func grouped(n int64) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + " " + s[i:]
	}
	return s
}
