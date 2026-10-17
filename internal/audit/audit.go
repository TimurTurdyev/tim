// Package audit: проверка сайта снаружи - то, что видит любой посетитель или бот. Решения - чистые
// функции от собранных фактов (заголовки, куки, ответы путей), сеть - в collect.go.
package audit

import (
	"cmp"
	"slices"

	"github.com/TimurTurdyev/tim/internal/level"
)

// Finding: что не так, почему это плохо и что сделать.
type Finding struct {
	Area  string      `json:"area"`
	Level level.Level `json:"level"`
	Text  string      `json:"text"`
	Why   string      `json:"why,omitempty"`
	Fix   string      `json:"fix,omitempty"`
}

// Report: итог проверки одного сайта.
type Report struct {
	Target   string    `json:"target"`
	Final    string    `json:"final"`
	Status   int       `json:"status"`
	Findings []Finding `json:"findings"`
	Checked  []string  `json:"checked"`
}

// Verdict: худший уровень находок; без находок и с одними сведениями - OK.
func (r *Report) Verdict() level.Level {
	v := level.OK
	for _, f := range r.Findings {
		v = max(v, f.Level)
	}
	return v
}

// sortFindings: худшие сверху, внутри уровня - в порядке проверок.
func sortFindings(f []Finding) {
	slices.SortStableFunc(f, func(a, b Finding) int { return cmp.Compare(b.Level, a.Level) })
}
