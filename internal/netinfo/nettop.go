package netinfo

import (
	"sort"
	"strconv"
	"strings"
)

// Flow: сколько процесс принял и отправил за интервал.
type Flow struct {
	Name string
	PID  string
	In   int64
	Out  int64
}

// ParseNettop разбирает nettop -P -L 2 -d -x -J bytes_in,bytes_out. Первый блок - накопленное
// с запуска процесса, второй - разница за интервал; берём последний блок. Сортировка по сумме.
func ParseNettop(out string) []Flow {
	blocks := strings.Split(out, ",bytes_in,bytes_out,")
	last := blocks[len(blocks)-1]
	var list []Flow
	for _, line := range strings.Split(last, "\n") {
		f := strings.Split(strings.TrimSpace(line), ",")
		if len(f) < 3 || f[0] == "" {
			continue
		}
		in, err1 := strconv.ParseInt(f[1], 10, 64)
		outB, err2 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || in+outB == 0 {
			continue
		}
		name, pid := f[0], ""
		if i := strings.LastIndex(f[0], "."); i > 0 {
			name, pid = f[0][:i], f[0][i+1:]
		}
		list = append(list, Flow{Name: name, PID: pid, In: in, Out: outB})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].In+list[i].Out > list[j].In+list[j].Out })
	return list
}
