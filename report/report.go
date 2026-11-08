// Package report: что выяснила команда - данные для JSON и блоки для глаз. Отчёт рисуют экраны tim и
// страница проверки сайта, поэтому пакет открыт другим модулям и ничего не знает о терминале.
package report

import "fmt"

var levelWords = [...]string{"skip", "info", "ok", "warn", "bad"}

// Level: насколько всё плохо; порядок важен, худший уровень - наибольший.
type Level int

const (
	Skip Level = iota // проверить нечем, например у домена нет почты
	Info              // к сведению: не плохо и не хорошо
	OK
	Warn
	Bad
)

func (l Level) String() string {
	switch l {
	case Bad:
		return "плохо"
	case Warn:
		return "внимание"
	case OK:
		return "ок"
	}
	return "-"
}

// MarshalText: в JSON уровень словом, а не числом.
func (l Level) MarshalText() ([]byte, error) {
	if l < Skip || l > Bad {
		return nil, fmt.Errorf("неизвестный уровень %d", int(l))
	}
	return []byte(levelWords[l]), nil
}

// UnmarshalText: уровень словом обратно - кэш проверок читается тем же типом, что пишется.
func (l *Level) UnmarshalText(b []byte) error {
	for i, w := range levelWords {
		if string(b) == w {
			*l = Level(i)
			return nil
		}
	}
	return fmt.Errorf("неизвестный уровень %q", b)
}

// Worst: худший из уровней, Skip без уровней.
func Worst(levels ...Level) Level {
	worst := Skip
	for _, l := range levels {
		worst = max(worst, l)
	}
	return worst
}

// Finding: что не так, почему это плохо и что сделать.
type Finding struct {
	Area    string `json:"area"`
	Level   Level  `json:"level"`
	Text    string `json:"text"`
	Why     string `json:"why,omitempty"`
	Fix     string `json:"fix,omitempty"`
	Subject string `json:"subject,omitempty"` // о ком, когда в отчёте их несколько: сайт, сервер, порт
	// Intrusive: нашлось запросом, которого обычный посетитель не делает (путь .env, порт базы).
	Intrusive bool `json:"intrusive,omitempty"`
}

// Report: что показать человеку и что отдать в JSON.
type Report struct {
	Schema  string  // формат JSON вида tim.audit/1; меняется только с номером
	Title   string  // первая строка: сайт, хост; пусто - без заголовка
	Verdict Verdict // итог рядом с заголовком
	Data    any     // что уходит в --json: форма - контракт для агентов и страницы проверки
	Blocks  []Block
}

// Verdict: итог отчёта одной строкой; пустой текст - итога нет.
type Verdict struct {
	Level Level
	Text  string
}

// Block: часть отчёта для глаз. Виды - ниже, других нет.
type Block interface{ block() }

// Heading: заголовок группы: всё до следующего заголовка относится к нему.
type Heading string

// Facts: пары "что - значение": записи DNS, карточка процесса.
type Facts []Fact

// Fact: Level Info приглушает значение, Warn и Bad красят; остальные уровни - как есть.
type Fact struct {
	Key, Value string
	Level      Level
}

// Table: строки с колонками. В ячейках значения как есть (число, байты, длительность): их форматирует
// рендерер и по ним сортирует экран.
type Table struct {
	Columns []Column
	Rows    [][]any
}

// Mark: ячейка со своим уровнем - состояние строки красится им, а не стилем колонки.
type Mark struct {
	Text  string
	Level Level
}

// Column: Kind говорит, как печатать и сравнивать значения, Style - как выделить колонку.
type Column struct {
	Title string
	Kind  Kind
	Style Style
}

// Kind: вид значений колонки. Числа, байты и длительности - вправо, ноль - пустая ячейка.
type Kind int

const (
	Text     Kind = iota // string
	Number               // int
	Bytes                // int64
	Duration             // time.Duration
)

// Style: Key - ярлык строки (порт, имя), Muted - второстепенное.
type Style int

const (
	Plain Style = iota
	Key
	Muted
)

// Findings: находки - худшие сверху, у каждой почему и что сделать.
type Findings []Finding

// Note: строка мелким шрифтом - что проверено, что скрыто, откуда данные.
type Note string

// Next: что сделать дальше. Шаги с командой tim экран запускает цифрами.
type Next []Step

// Step: Text - что это даст, Command - команда целиком ("tim port 8000 kill").
type Step struct {
	Text, Command string
}

func (Heading) block()  {}
func (Facts) block()    {}
func (Table) block()    {}
func (Findings) block() {}
func (Note) block()     {}
func (Next) block()     {}
