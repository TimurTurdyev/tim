package sites

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cors"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/og"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/view"
	"github.com/TimurTurdyev/tim/internal/watch"
	"github.com/TimurTurdyev/tim/report"
)

// keyHeaders: заголовки ответа, по которым видно главное.
var keyHeaders = []string{"Server", "Content-Type", "Content-Encoding", "Cache-Control", "Location", "Strict-Transport-Security", "X-Powered-By"}

// printTimings: сколько отвечает сайт по фазам; follow - идти по редиректам, до десяти.
// timingJSON: один переход для --json, время в миллисекундах.
type timingJSON struct {
	URL       string      `json:"url"`
	Status    int         `json:"status"`
	Proto     string      `json:"proto"`
	Size      int64       `json:"size"`
	DNSMs     int64       `json:"dns_ms"`
	ConnectMs int64       `json:"connect_ms"`
	TLSMs     int64       `json:"tls_ms"`
	TTFBMs    int64       `json:"ttfb_ms"`
	TotalMs   int64       `json:"total_ms"`
	Location  string      `json:"location,omitempty"`
	Headers   http.Header `json:"headers"`
}

func ms(d time.Duration) string { return fmt.Sprintf("%d мс", d.Milliseconds()) }

// watchSite: опрос раз в every; на каждой смене состояния - строка и уведомление системы.
func watchSite(arg string, o watchOptions) error {
	url := withScheme(arg)
	client := &http.Client{Timeout: o.timeout}
	var tr watch.Tracker
	probes, fails := 0, 0
	start := time.Now()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	ticker := time.NewTicker(o.every)
	defer ticker.Stop()
	ui.Println(ui.Dim("слежу за " + url + " раз в " + o.every.String() + "; Ctrl-C - выход"))
	for {
		ok, detail, err := watch.Probe(client, url)
		if err != nil {
			detail = ui.ShortErr(err)
		}
		probes++
		if !ok {
			fails++
		}
		if ev, changed := tr.Observe(ok, time.Now()); changed {
			reportChange(url, ev, detail, o.quiet)
		}
		select {
		case <-stop:
			ui.Println()
			ui.Println(ui.Dim(fmt.Sprintf("опросов %d, неудачных %d за %s", probes, fails, ui.Duration(time.Since(start)))))
			return nil
		case <-ticker.C:
		}
	}
}

func reportChange(url string, ev watch.Event, detail string, quiet bool) {
	line := ev.At.Format("15:04:05") + "  "
	switch ev.To {
	case watch.Up:
		line += ui.Label("работает") + "  " + detail
		if ev.From == watch.Down {
			line += ui.Dim("  лежал " + ui.Duration(ev.Was))
			if !quiet {
				schedule.Notify("tim watch: поднялся", url+" после "+ui.Duration(ev.Was))
			}
		}
	case watch.Down:
		line += ui.Bad("лежит") + "  " + detail
		if !quiet {
			schedule.Notify("tim watch: лежит", url+": "+detail)
		}
	}
	ui.Println(line)
}

func withScheme(url string) string {
	if !strings.Contains(url, "://") {
		return "https://" + url
	}
	return url
}

// printTimings: время ответа по фазам и главные заголовки; с follow - по каждому редиректу.
func printTimings(arg string, follow, allHeaders, asJSON bool) error {
	url := withScheme(arg)
	var hops []timingJSON
	var blocks []report.Block
	for range 10 {
		t, err := inspect.Measure(url)
		if err != nil {
			return ui.Errorf("%s: %v", url, err)
		}
		hops = append(hops, timingJSON{URL: url, Status: t.Status, Proto: t.Proto, Size: t.Size, DNSMs: t.DNS.Milliseconds(),
			ConnectMs: t.Connect.Milliseconds(), TLSMs: t.TLS.Milliseconds(), TTFBMs: t.TTFB.Milliseconds(), TotalMs: t.All.Milliseconds(),
			Location: t.Location, Headers: t.Header})
		status := report.Fact{Key: "ответ", Value: fmt.Sprintf("%d %s, %s", t.Status, t.Proto, ui.Bytes(t.Size)), Level: report.OK}
		switch {
		case t.Status >= 500:
			status.Level = report.Bad
		case t.Status >= 400:
			status.Level = report.Warn
		}
		facts := report.Facts{status,
			{Key: "по фазам", Value: fmt.Sprintf("dns %s  tcp %s  tls %s  первый байт %s  загрузка %s", ms(t.DNS), ms(t.Connect), ms(t.TLS), ms(t.TTFB), ms(t.All-t.TTFB))},
			{Key: "всего", Value: ms(t.All)}}
		blocks = append(blocks, report.Heading(url), facts, headerFacts(t.Header, allHeaders))
		if !follow || t.Location == "" {
			if t.Location != "" {
				blocks = append(blocks, report.Next{{Text: "по редиректу", Command: "tim http -L " + arg}})
			}
			return view.Show(report.Report{Schema: "tim.http/1", Data: hops, Blocks: blocks}, asJSON)
		}
		url = t.Location
	}
	return ui.Errorf("больше 10 редиректов")
}

// headerFacts: главные заголовки ответа, с all - все по алфавиту.
func headerFacts(h http.Header, all bool) report.Facts {
	names := keyHeaders
	if all {
		names = slices.Sorted(maps.Keys(h))
	}
	var out report.Facts
	for _, k := range names {
		if v := h.Get(k); v != "" {
			out = append(out, report.Fact{Key: k, Value: v, Level: report.Info})
		}
	}
	return out
}

// printPreview: как ссылка развернётся в мессенджере - карточка и что в ней не так.
func printPreview(arg string, asJSON bool) error {
	url := withScheme(arg)
	card, status, err := og.Fetch(url)
	if err != nil {
		return ui.Errorf("%s: %v", url, ui.ShortErr(err))
	}
	out := struct {
		URL      string       `json:"url"`
		Status   int          `json:"status"`
		Card     og.Card      `json:"card"`
		Image    string       `json:"image,omitempty"`
		Problems []og.Problem `json:"problems"`
	}{URL: url, Status: status, Card: card, Problems: og.Check(card)}
	facts := report.Facts{cardFact("сайт", card.SiteName, ""), cardFact("заголовок", card.Title, card.HTMLTitle),
		cardFact("описание", card.Description, card.MetaDescription)}
	if status != 200 {
		facts = append(report.Facts{{Key: "ответ", Value: fmt.Sprintf("%d боту мессенджера - превью может не собраться", status), Level: report.Warn}}, facts...)
	}
	if card.Image != "" {
		out.Image = og.ImageURL(url, card)
		info, more := og.CheckImage(out.Image)
		out.Problems = append(out.Problems, more...)
		facts = append(facts, report.Fact{Key: "картинка", Value: out.Image})
		if info != "" {
			facts = append(facts, report.Fact{Key: "", Value: info, Level: report.Info})
		}
	}
	r := report.Report{Schema: "tim.og/1", Title: url, Data: out, Blocks: []report.Block{facts}}
	if len(out.Problems) == 0 {
		r.Verdict = report.Verdict{Level: report.OK, Text: "превью в порядке"}
		return view.Show(r, asJSON)
	}
	bad := 0
	var found report.Findings
	for _, p := range out.Problems {
		f := report.Finding{Level: report.Warn, Text: p.Text}
		if p.Bad {
			bad++
			f.Level = report.Bad
		}
		found = append(found, f)
	}
	r.Blocks = append(r.Blocks, found, report.Note("Telegram кеширует превью: после правки отправь ссылку @WebpageBot, чтобы обновить"))
	if err := view.Show(r, asJSON); err != nil {
		return err
	}
	if bad > 0 {
		return ui.Errorf("превью соберётся плохо")
	}
	return nil
}

// cardFact: поле карточки; пустое - запасным вариантом из страницы или прочерком.
func cardFact(label, v, fallback string) report.Fact {
	switch {
	case v != "":
		return report.Fact{Key: label, Value: v}
	case fallback != "":
		return report.Fact{Key: label, Value: fallback + "  (запасной вариант)", Level: report.Info}
	}
	return report.Fact{Key: label, Value: "-", Level: report.Info}
}

// checkCORS: пустит ли браузер запрос с origin - preflight, ответ и чего не хватает.
func checkCORS(arg string, o corsOptions) error {
	if o.origin == "" {
		return ui.Errorf("откуда запрос: --origin https://app.example.com")
	}
	r := cors.Request{Origin: strings.TrimSuffix(o.origin, "/"), Method: strings.ToUpper(o.method), Headers: o.headers, Credentials: o.credentials}
	resp, err := cors.Probe(withScheme(arg), r)
	if se, ok := errors.AsType[*cors.StepError](err); ok {
		return ui.Errorf("%s: %v", se.Step, ui.ShortErr(se.Err))
	}
	if err != nil {
		return err
	}
	probs := cors.Analyze(r, resp.PreflightCode, resp.Preflight, resp.Actual)
	allowed := true
	var found report.Findings
	for _, p := range probs {
		allowed = allowed && !p.Bad
		f := report.Finding{Level: report.Warn, Text: p.Text}
		if p.Bad {
			f.Level = report.Bad
		}
		found = append(found, f)
	}
	data := struct {
		Origin          string         `json:"origin"`
		Method          string         `json:"method"`
		PreflightStatus string         `json:"preflight_status,omitempty"`
		Preflight       http.Header    `json:"preflight,omitempty"`
		Status          string         `json:"status"`
		Headers         http.Header    `json:"headers"`
		Problems        []cors.Problem `json:"problems"`
		Allowed         bool           `json:"allowed"`
	}{r.Origin, r.Method, resp.PreflightStatus, resp.Preflight, resp.Status, resp.Actual, probs, allowed}
	var blocks []report.Block
	if resp.Preflight != nil {
		blocks = append(blocks, report.Heading("preflight  OPTIONS "+resp.PreflightStatus), headerValues(resp.Preflight, "Access-Control-Allow-Origin",
			"Access-Control-Allow-Methods", "Access-Control-Allow-Headers", "Access-Control-Allow-Credentials", "Access-Control-Max-Age"))
	}
	blocks = append(blocks, report.Heading("ответ  "+resp.Method+" "+resp.Status), headerValues(resp.Actual, "Access-Control-Allow-Origin",
		"Access-Control-Allow-Credentials", "Access-Control-Expose-Headers", "Vary"))
	rep := report.Report{Schema: "tim.cors/1", Data: data, Blocks: blocks}
	if len(found) == 0 {
		rep.Blocks = append(rep.Blocks, report.Facts{{Key: "итог", Value: "браузер пустит " + r.Method + " с " + r.Origin, Level: report.OK}})
	} else {
		rep.Blocks = append(rep.Blocks, report.Heading("итог"), found)
	}
	if err := view.Show(rep, o.json); err != nil {
		return err
	}
	if !allowed {
		return ui.Errorf("браузер не пустит %s с %s", r.Method, r.Origin)
	}
	return nil
}

func headerValues(h http.Header, keys ...string) report.Facts {
	var out report.Facts
	for _, k := range keys {
		if v := strings.Join(h.Values(k), ", "); v != "" {
			out = append(out, report.Fact{Key: k, Value: v})
		}
	}
	if len(out) == 0 {
		out = append(out, report.Fact{Key: "заголовки CORS", Value: "нет", Level: report.Info})
	}
	return out
}
