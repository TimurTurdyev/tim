package sites

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/cors"
	"github.com/TimurTurdyev/tim/internal/inspect"
	"github.com/TimurTurdyev/tim/internal/og"
	"github.com/TimurTurdyev/tim/internal/schedule"
	"github.com/TimurTurdyev/tim/internal/ui"
	"github.com/TimurTurdyev/tim/internal/watch"
)

// keyHeaders: заголовки ответа, по которым видно главное.
var keyHeaders = []string{"Server", "Content-Type", "Content-Encoding", "Cache-Control", "Location", "Strict-Transport-Security", "X-Powered-By"}

// printTimings: сколько отвечает сайт по фазам; follow - идти по редиректам, до десяти.
func printTimings(arg string, follow, allHeaders bool) error {
	url := withScheme(arg)
	for range 10 {
		t, err := inspect.Measure(url)
		if err != nil {
			return ui.Errorf("%s: %v", url, err)
		}
		status := fmt.Sprintf("%d %s", t.Status, t.Proto)
		switch {
		case t.Status >= 500:
			status = ui.Bad(status)
		case t.Status >= 400:
			status = ui.Warn(status)
		default:
			status = ui.Label(status)
		}
		ui.Println(status + "  " + url + ui.Dim("  "+ui.Bytes(t.Size)))
		ui.Println(ui.Dim(fmt.Sprintf("  dns %s  tcp %s  tls %s  первый байт %s  загрузка %s",
			ms(t.DNS), ms(t.Connect), ms(t.TLS), ms(t.TTFB), ms(t.All-t.TTFB))) + "  " + ui.Label("всего "+ms(t.All)))
		printHeaders(t.Header, allHeaders)
		if !follow || t.Location == "" {
			if t.Location != "" {
				ui.Println(ui.Dim("редирект: tim http -L " + arg))
			}
			return nil
		}
		url = t.Location
	}
	return ui.Errorf("больше 10 редиректов")
}

func printHeaders(h http.Header, all bool) {
	names := keyHeaders
	if all {
		names = nil
		for k := range h {
			names = append(names, k)
		}
		sort.Strings(names)
	}
	for _, k := range names {
		if v := h.Get(k); v != "" {
			ui.Println("  " + ui.Pad(k, 26) + " " + v)
		}
	}
}

func ms(d time.Duration) string { return fmt.Sprintf("%d мс", d.Milliseconds()) }

// printPreview: как ссылка будет выглядеть в Telegram и соцсетях.
func printPreview(arg string) error {
	url := withScheme(arg)
	card, status, err := og.Fetch(url)
	if err != nil {
		return ui.Errorf("%s: %v", url, ui.ShortErr(err))
	}
	if status != 200 {
		ui.Println(ui.Warn(fmt.Sprintf("страница отвечает %d боту мессенджера - превью может не собраться", status)))
	}
	show("сайт", card.SiteName, "")
	show("заголовок", card.Title, card.HTMLTitle)
	show("описание", card.Description, card.MetaDescription)
	probs := og.Check(card)
	if card.Image != "" {
		img := og.ImageURL(url, card)
		info, more := og.CheckImage(img)
		show("картинка", img, "")
		if info != "" {
			ui.Println(ui.Pad("", 11) + ui.Dim(info))
		}
		probs = append(probs, more...)
	}
	if len(probs) == 0 {
		ui.Println(ui.Label("превью в порядке"))
		return nil
	}
	ui.Println()
	bad := 0
	for _, p := range probs {
		if p.Bad {
			bad++
			ui.Println(ui.Bad(p.Text))
		} else {
			ui.Println(ui.Warn(p.Text))
		}
	}
	ui.Println(ui.Dim("Telegram кеширует превью: после правки отправь ссылку @WebpageBot, чтобы обновить"))
	if bad > 0 {
		return ui.Errorf("превью соберётся плохо")
	}
	return nil
}

func show(label, v, fallback string) {
	switch {
	case v != "":
		ui.Println(ui.Label(ui.Pad(label, 11)) + v)
	case fallback != "":
		ui.Println(ui.Label(ui.Pad(label, 11)) + ui.Dim(fallback+"  (запасной вариант)"))
	default:
		ui.Println(ui.Label(ui.Pad(label, 11)) + ui.Dim("-"))
	}
}

// checkCORS: пустит ли браузер запрос с origin, а если нет - какой заголовок мешает.
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
	if resp.Preflight != nil {
		ui.Println(ui.Label("preflight") + "  OPTIONS " + resp.PreflightStatus)
		printHeaderValues(resp.Preflight, "Access-Control-Allow-Origin", "Access-Control-Allow-Methods", "Access-Control-Allow-Headers",
			"Access-Control-Allow-Credentials", "Access-Control-Max-Age")
	}
	ui.Println(ui.Label("ответ") + "      " + resp.Method + " " + resp.Status)
	printHeaderValues(resp.Actual, "Access-Control-Allow-Origin", "Access-Control-Allow-Credentials", "Access-Control-Expose-Headers", "Vary")
	probs := cors.Analyze(r, resp.PreflightCode, resp.Preflight, resp.Actual)
	ui.Println()
	if len(probs) == 0 {
		ui.Println(ui.Label("браузер пустит " + r.Method + " с " + r.Origin))
		return nil
	}
	bad := 0
	for _, p := range probs {
		if p.Bad {
			bad++
			ui.Println(ui.Bad(p.Text))
		} else {
			ui.Println(ui.Warn(p.Text))
		}
	}
	if bad > 0 {
		return ui.Errorf("браузер не пустит %s с %s", r.Method, r.Origin)
	}
	return nil
}

func printHeaderValues(h http.Header, keys ...string) {
	for _, k := range keys {
		if v := strings.Join(h.Values(k), ", "); v != "" {
			ui.Println(ui.Dim("  " + k + ": " + v))
		}
	}
}

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
