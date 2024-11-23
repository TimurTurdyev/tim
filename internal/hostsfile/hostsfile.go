// Package hostsfile: блок записей tim внутри /etc/hosts и работа с ним.
// Всё, что вне блока, остаётся нетронутым: в этом файле бывают чужие записи.
package hostsfile

import (
	"sort"
	"strings"
)

const (
	Begin           = "# tim:begin"
	End             = "# tim:end"
	offPrefix       = "# tim-off "
	DefaultProfile  = "default"
	profileSplitter = " # tim "
)

// Entry: одна строка блока.
type Entry struct {
	IP      string
	Names   []string
	Profile string
	On      bool
}

func (e Entry) String() string {
	line := e.IP + " " + strings.Join(e.Names, " ") + profileSplitter + e.Profile
	if !e.On {
		return offPrefix + line
	}
	return line
}

// Parse достаёт записи tim из текста /etc/hosts.
func Parse(text string) []Entry {
	var out []Entry
	inside := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == Begin:
			inside = true
			continue
		case trimmed == End:
			inside = false
			continue
		case !inside || trimmed == "":
			continue
		}
		if e, ok := parseLine(trimmed); ok {
			out = append(out, e)
		}
	}
	return out
}

func parseLine(line string) (Entry, bool) {
	e := Entry{On: true, Profile: DefaultProfile}
	if rest, found := strings.CutPrefix(line, offPrefix); found {
		e.On, line = false, strings.TrimSpace(rest)
	}
	if body, profile, found := strings.Cut(line, profileSplitter); found {
		line = strings.TrimSpace(body)
		if p := strings.TrimSpace(profile); p != "" {
			e.Profile = p
		}
	}
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return e, false
	}
	e.IP, e.Names = fields[0], fields[1:]
	return e, true
}

// Render возвращает текст /etc/hosts с заменённым блоком tim. Пустой список убирает блок целиком.
func Render(text string, entries []Entry) string {
	var kept []string
	inside := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == Begin:
			inside = true
		case trimmed == End:
			inside = false
		case !inside:
			kept = append(kept, line)
		}
	}
	for len(kept) > 0 && strings.TrimSpace(kept[len(kept)-1]) == "" {
		kept = kept[:len(kept)-1]
	}
	if len(entries) == 0 {
		return strings.Join(kept, "\n") + "\n"
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Profile != entries[j].Profile {
			return entries[i].Profile < entries[j].Profile
		}
		return entries[i].Names[0] < entries[j].Names[0]
	})
	block := []string{Begin}
	for _, e := range entries {
		block = append(block, e.String())
	}
	block = append(block, End)
	return strings.Join(append(kept, append([]string{""}, block...)...), "\n") + "\n"
}

// Add добавляет имена. Имя может быть только в одной записи, поэтому старое вхождение убирается.
func Add(entries []Entry, ip string, names []string, profile string) []Entry {
	if profile == "" {
		profile = DefaultProfile
	}
	entries = Remove(entries, names)
	for i, e := range entries {
		if e.IP == ip && e.Profile == profile && e.On {
			entries[i].Names = merge(e.Names, names)
			return entries
		}
	}
	return append(entries, Entry{IP: ip, Names: names, Profile: profile, On: true})
}

func merge(a, b []string) []string {
	seen := map[string]bool{}
	for _, n := range a {
		seen[n] = true
	}
	for _, n := range b {
		if !seen[n] {
			seen[n] = true
			a = append(a, n)
		}
	}
	return a
}

// Remove убирает имена из всех записей; опустевшие записи исчезают.
func Remove(entries []Entry, names []string) []Entry {
	drop := map[string]bool{}
	for _, n := range names {
		drop[n] = true
	}
	out := entries[:0:0]
	for _, e := range entries {
		var kept []string
		for _, n := range e.Names {
			if !drop[n] {
				kept = append(kept, n)
			}
		}
		if len(kept) > 0 {
			e.Names = kept
			out = append(out, e)
		}
	}
	return out
}

// Switch включает или выключает все записи профиля; второй результат - сколько записей поменялось.
func Switch(entries []Entry, profile string, on bool) ([]Entry, int) {
	n := 0
	for i, e := range entries {
		if e.Profile == profile && e.On != on {
			entries[i].On = on
			n++
		}
	}
	return entries, n
}

// Profiles: имена профилей и сколько в каждом включённых записей из скольких.
func Profiles(entries []Entry) ([]string, map[string][2]int) {
	counts := map[string][2]int{}
	for _, e := range entries {
		c := counts[e.Profile]
		c[1]++
		if e.On {
			c[0]++
		}
		counts[e.Profile] = c
	}
	names := make([]string, 0, len(counts))
	for p := range counts {
		names = append(names, p)
	}
	sort.Strings(names)
	return names, counts
}
