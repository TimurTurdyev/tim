package procs

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// All: все процессы системы с командными строками и загрузкой CPU за последние полсекунды.
func All() ([]Full, error) {
	out, err := runner.Output("ps", PsAll("pid=,ppid=,uid=,rss=,pcpu=,etime=,state=,comm=")...)
	if err != nil {
		return nil, err
	}
	list := withArgs(ParseFull(out))
	if recent := RecentCPU(500 * time.Millisecond); recent != nil {
		for i := range list {
			list[i].CPU = recent[list[i].PID]
		}
	}
	return list, nil
}

// withArgs дописывает процессам командные строки: без них эмулятор Android не отличить от любого qemu.
func withArgs(list []Full) []Full {
	out, err := runner.Output("ps", PsAll("pid=,args=")...)
	if err != nil {
		return list
	}
	args := ParseArgs(out)
	for i := range list {
		list[i].Args = args[list[i].PID]
	}
	return list
}
