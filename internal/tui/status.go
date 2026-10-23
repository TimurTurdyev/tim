package tui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Строка состояния - последняя строка тела на всех экранах. Одна грамматика: знак, "<объект>: <итог>"
// и время; у работы - "<что делаю>" и идущие секунды. Та же строка
// показывает фоновую работу, даже если экран, который её начал, уже закрыт.

// moodAfter: сколько маскот помнит исход действия.
const moodAfter = 30 * time.Second

// outcome: исход последнего действия сессии - маскот полминуты рад или расстроен.
var outcome struct {
	ok bool
	at time.Time
}

// JobDone: фоновая работа закончилась. Приходит экрану, который её запустил, если он ещё открыт;
// исход уже написан в строке состояния.
type JobDone struct {
	Text string
	Err  error
}

type (
	sayExpireMsg struct{ id int }
	jobDoneMsg   struct {
		id   int
		done JobDone
	}
)

// sayState: строка состояния - исход действия, предупреждение, ошибка или подсказка.
type sayState struct {
	text  string
	lvl   level.Level
	plain bool // серая подсказка без знака
	id    int
}

// job: фоновая работа, которую видно в строке состояния, пока она идёт.
type job struct {
	id, owner int
	text      string
	start     time.Time
}

// markOutcome запоминает исход действия для настроения маскота.
func markOutcome(ok bool) {
	outcome.ok, outcome.at = ok, time.Now()
}

// sessionMood: настроение машины, а сразу после действия - его исход.
func sessionMood(base brand.Mood) brand.Mood {
	switch {
	case time.Since(outcome.at) >= moodAfter:
		return base
	case outcome.ok:
		return brand.Happy
	}
	return brand.Upset
}

// Say пишет в строку состояния: успех гаснет через 5 с, предупреждение через 8 с или от клавиши, ошибка висит до
// следующей клавиши и её не съедает. Грамматика текста: "<объект>: <итог>".
func (a *App) Say(l level.Level, text string) tea.Cmd {
	switch l {
	case level.Bad:
		a.lastErr = text
		markOutcome(false)
	case level.OK:
		markOutcome(true)
	}
	a.say = sayState{text: text, lvl: l, id: a.say.id + 1}
	switch l {
	case level.Bad:
		return nil
	case level.Warn:
		return a.expire(8 * time.Second)
	}
	return a.expire(5 * time.Second)
}

// hint: серая подсказка в строке состояния на d.
func (a *App) hint(text string, d time.Duration) tea.Cmd {
	a.say = sayState{text: text, plain: true, id: a.say.id + 1}
	return a.expire(d)
}

func (a *App) expire(d time.Duration) tea.Cmd {
	id := a.say.id
	return tea.Tick(d, func(time.Time) tea.Msg { return sayExpireMsg{id} })
}

// LastError: полный текст последней ошибки этой сессии.
func (a *App) LastError() string { return a.lastErr }

// Job запускает работу в фоне. Пока она идёт, строка состояния показывает what с идущими
// секундами, клавиши экрана стоят; исход - там же, текст или ошибка. Экран получает
// JobDone, если ещё открыт; закрытый экран работу не отменяет.
func (a *App) Job(what string, fn func() (string, error)) tea.Cmd {
	a.lastJob++
	j := job{id: a.lastJob, owner: a.top().id, text: what, start: time.Now()}
	a.jobs = append(a.jobs, j)
	return func() tea.Msg {
		text, err := fn()
		return jobDoneMsg{id: j.id, done: JobDone{Text: text, Err: err}}
	}
}

// finish: работа закончилась - исход в строку состояния.
func (a *App) finish(m jobDoneMsg) tea.Cmd {
	var took time.Duration
	for i, j := range a.jobs {
		if j.id == m.id {
			took = time.Since(j.start)
			a.jobs = append(a.jobs[:i], a.jobs[i+1:]...)
			break
		}
	}
	switch {
	case m.done.Err != nil:
		return a.Say(level.Bad, m.done.Err.Error())
	case m.done.Text != "" && took < 100*time.Millisecond:
		return a.Say(level.OK, m.done.Text)
	case m.done.Text != "":
		return a.Say(level.OK, m.done.Text+" "+symSep+" "+tookText(took))
	}
	return nil
}

// tookText: сколько шла работа - "0.4 с", "4 с", "2 мин".
func tookText(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%.1f с", d.Seconds())
	}
	return ui.Short(d)
}

// busy: верхний экран ждёт свою работу - его клавиши стоят.
func (a *App) busy() bool {
	id := a.top().id
	for _, j := range a.jobs {
		if j.owner == id {
			return true
		}
	}
	return false
}

// statusLine: последняя строка тела. Ошибка важнее идущей работы, работа - остального.
func (a *App) statusLine(w int) string {
	text := ""
	switch {
	case a.say.text != "" && a.say.lvl == level.Bad && !a.say.plain:
		text = styleSayBad.Render(symCross + " " + a.say.text)
	case len(a.jobs) > 0:
		j := a.jobs[len(a.jobs)-1]
		line := Spinner() + " " + j.text
		if d := time.Since(j.start); d >= time.Second {
			line += " " + symSep + " " + ui.Short(d)
		}
		if n := len(a.jobs) - 1; n > 0 {
			line += " " + symSep + " ещё " + ui.Count(n, "работа", "работы", "работ")
		}
		text = styleSayWork.Render(line)
	case a.say.plain:
		text = styleDim.Render(a.say.text)
	case a.say.text != "":
		st, mark := styleSayOK, symCheck
		if a.say.lvl == level.Warn {
			st, mark = styleSayWarn, symWarn
		}
		text = st.Render(mark + " " + a.say.text)
	}
	if text == "" {
		return ""
	}
	return ui.Clip(" "+text, w)
}
