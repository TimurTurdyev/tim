package harvest

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	numberTop   = 30 // номер из одной-двух цифр перебирается от нуля до этого числа
	numberGap   = 20 // номер длиннее - только в окне вокруг своего значения
	suffixTop   = 5  // www -> www1..www5 и www01..www05
	suffixLabel = 20 // метке длиннее цифра на конец не дописывается
	digits      = "0123456789"
)

// Окружения, которые на замере дали живые имена; отдельной меткой находились только test и dev.
var (
	dashEnvs = []string{"dev", "test", "stage", "staging", "old", "new", "beta"}
	dotEnvs  = []string{"test", "dev"}
)

// Mutate: кандидаты по образцу имени name внутри domain - полные имена с другой левой меткой, без самого name.
func Mutate(name, domain string) []string {
	name, domain = hostName(name), hostName(domain)
	sub, inside := strings.CutSuffix(name, "."+domain)
	if domain == "" || !inside {
		return nil
	}
	label, _, _ := strings.Cut(sub, ".")
	if label[0] == '_' { // _dmarc и _spf - служебные записи, соседей-узлов у них нет
		return nil
	}
	rest := name[len(label):]
	seen := map[string]bool{name: true}
	var out []string
	for _, left := range variants(label) {
		candidate := hostName(left + rest)
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		out = append(out, candidate)
	}
	return out
}

// variants: чем заменить левую метку; у видов "метка.окружение" и "окружение.метка" замена состоит из двух меток.
func variants(label string) []string {
	out := renumbered(label)
	if !strings.ContainsAny(label, digits) && len(label) <= suffixLabel {
		for k := 1; k <= suffixTop; k++ {
			out = append(out, label+strconv.Itoa(k), label+"0"+strconv.Itoa(k))
		}
	}
	for _, env := range dashEnvs {
		out = append(out, label+"-"+env, env+"-"+label)
	}
	for _, env := range dotEnvs {
		out = append(out, label+"."+env, env+"."+label)
	}
	return out
}

// renumbered: метка с другим номером на месте каждого числа: mmd01 -> mmd00..mmd30, y2021 -> y2001..y2041.
func renumbered(label string) []string {
	var out []string
	for i := 0; i < len(label); {
		end := i
		for end < len(label) && strings.IndexByte(digits, label[end]) >= 0 {
			end++
		}
		if end == i {
			i++
			continue
		}
		number := label[i:end]
		if value, err := strconv.Atoi(number); err == nil {
			low, high := 0, numberTop
			if len(number) > 2 {
				low, high = max(0, value-numberGap), value+numberGap
			}
			for k := low; k <= high; k++ {
				out = append(out, fmt.Sprintf("%s%0*d%s", label[:i], len(number), k, label[end:]))
			}
		}
		i = end
	}
	return out
}
