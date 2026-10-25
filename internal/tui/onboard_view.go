package tui

import (
	"strings"

	"github.com/TimurTurdyev/tim/internal/brand"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Вид мастера: лок-ап с крупным маскотом и строкой шагов, вопрос шага, список, финал с тем, что
// записано и что набрать дальше.

var stepNames = []string{"папки", "проекты", "оболочка"}

// steps: строка шагов - пройденные с галочкой, текущий жирным, будущие серым.
func (o *onboard) steps() string {
	var parts []string
	for i, n := range stepNames {
		switch {
		case i < o.step || o.step == 3 && (i < 2 || o.shell != ""):
			parts = append(parts, styleGood.Render(symCheck)+" "+styleDim.Render(n))
		case o.step == 3:
			parts = append(parts, styleDim.Render("○ "+n))
		case i == o.step:
			parts = append(parts, styleBold.Render(n))
		default:
			parts = append(parts, styleDim.Render(n))
		}
	}
	return strings.Join(parts, styleDim.Render("  "+symPointer+"  "))
}

func (o *onboard) header(width, height int) []string {
	look := brand.Look{Mood: sessionMood(brand.Calm), Light: ui.Light()}
	if o.blink {
		look.Mood |= brand.Blink
	}
	lines := [][]string{{ui.Strong(brand.Name) + "  " + ui.Dim(o.opts.Build)}}
	switch o.step {
	case 3:
		look.Mood = brand.Happy
		lines = [][]string{{styleBold.Render("Готово. " + brand.Name + " знает твои проекты")},
			{styleDim.Render("первый запуск:  ") + o.steps()},
			{ui.Dim("конфиг " + o.opts.Config), ui.Dim("править руками можно, " + brand.Name + " перечитает")}}
	case 2:
		lines = append(lines, []string{styleDim.Render("первый запуск "+symSep+" шаг 3 из 3:  ") + o.steps()},
			[]string{ui.Dim("конфиг записан: " + o.opts.Config), ui.Dim("в реестре " + ui.Count(o.saved, "проект", "проекта", "проектов"))})
	default:
		lines = append(lines, []string{styleDim.Render("первый запуск "+symSep+" шаг "+itoa(o.step+1)+" из 3:  ") + o.steps()},
			[]string{ui.Dim("конфиг будет здесь: " + o.opts.Config), ui.Dim("править руками можно, " + brand.Name + " перечитает")})
	}
	large := height >= 30 && o.step < 3
	out := ui.Header(lines, ui.HeaderOpts{Look: look, Indent: 2, Width: width, Large: large, Sprite: width >= 70 && !dimmed, Keep: true})
	return append([]string{""}, out...)
}

func (o *onboard) View(width, height int) string {
	out := o.header(width, height)
	out = append(out, "")
	switch o.step {
	case 0:
		out = append(out, "   "+styleBold.Render("Где лежат твои проекты?"),
			"   "+styleDim.Render("tim заглянет в папку, найдёт репозитории и заведёт реестр: имя, стек, порт, папка. До шага 2 ничего не пишется."),
			"", "   "+styleHead.Render("ПАПКИ С КОДОМ"))
	case 1:
		out = append(out, "   "+styleBold.Render("Нашёл "+ui.Count(len(o.found), "репозиторий", "репозитория", "репозиториев")+" в "+o.root),
			"   "+styleDim.Render("отмеченное попадёт в реестр; без отметки - то, что не менялось больше года"),
			"", "   "+styleHead.Render("   "+ui.Fit("ИМЯ", 26)+ui.Fit("СТЕК", 11)+ui.Fit("МЕНЯЛСЯ", 10)+"ПАПКА")+styleDim.Render("   отмечено "+itoa(len(o.chosen()))))
	case 2:
		if o.opts.Shell == nil {
			out = append(out, "   "+styleBold.Render("Подключить tim к оболочке?"),
				"   "+styleAmber.Render(symWarn+" "+o.opts.ShellFile),
				"   "+styleDim.Render("без подключения не будут работать tim go (переход в папку) и дополнение по tab; остальное работает"),
				"", "   "+styleHead.Render("ОБОЛОЧКА"))
			break
		}
		out = append(out, "   "+styleBold.Render("Подключить tim к оболочке?"),
			"   "+styleDim.Render("В "+o.opts.ShellFile+" допишется одна строка, всё остальное tim держит у себя и обновляет сам:"),
			"", "     "+styleText.Render(o.opts.ShellLine), "",
			"   "+styleDim.Render(ui.Fit("$ tim go <проект>", 26)+"переходит в папку проекта, а не печатает путь"),
			"   "+styleDim.Render(ui.Fit("tab", 26)+"дополняет команды tim"),
			"   "+styleDim.Render(ui.Fit("cd в папку проекта", 26)+"подсказка: какой это проект"),
			"", "   "+styleHead.Render("ОБОЛОЧКА"))
	case 3:
		return strings.Join(append(out, o.final()...), "\n")
	}
	if o.busy != "" {
		out = append(out, "   "+styleDim.Render(Spinner()+" "+o.busy))
		return strings.Join(out, "\n")
	}
	listH := max(3, height-len(out)-3)
	out = append(out, o.list.View(width, listH)...)
	if o.step == 0 && o.input.Focused() {
		out = append(out, "", "   "+o.input.View())
	}
	if o.step == 1 && len(o.found) == 0 {
		out = append(out, "   "+styleDim.Render("репозиториев не нашёл: конфиг запишется с пустым реестром"))
	}
	if o.step == 2 && o.opts.Shell != nil {
		out = append(out, "", "   "+styleDim.Render("Убрать подключение - стереть эту строку из "+o.opts.ShellFile+"; больше tim в оболочке ничего не трогает."))
	}
	return strings.Join(out, "\n")
}

// final: что записано и первые три команды.
func (o *onboard) final() []string {
	label := func(s string) string { return "   " + styleAccent.Render(ui.Fit(s, 12)) }
	shell := ui.Dim("не подключена " + symSep + " tim init setup")
	if o.shell != "" {
		shell = o.opts.ShellFile + " + " + o.opts.ShellLine + ui.Dim(" "+symSep+" заработает в новом окне терминала")
	}
	out := []string{"   " + styleHead.Render("ЗАПИСАНО"),
		label("реестр") + ui.Count(o.saved, "проект", "проекта", "проектов") + " из " + o.root,
		label("порты") + ui.Dim("свой у каждого dev-сервера: из настроек проекта, иначе с 8000"),
		label("оболочка") + shell,
		"", "   " + styleHead.Render("ЧТО ДАЛЬШЕ")}
	for _, c := range [][2]string{{"go", "перейти в проект, недавние сверху"}, {"serve -d", "поднять dev-сервер проекта в фоне на его порту"}, {"status", "сводка: машина, проект, сеть"}} {
		out = append(out, "   "+styleDim.Render("$ tim ")+ui.Fit(c[0], 14)+styleDim.Render(c[1]))
	}
	return append(out, "", "   "+styleDim.Render(symArrow+" enter - главный экран tim: то же самое буквами и поиском"))
}
