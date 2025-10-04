package cmd

import (
	"fmt"
	"time"

	"github.com/TimurTurdyev/tim/internal/procs"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// withArgs дописывает процессам командные строки: без них эмулятор Android не отличить от любого qemu.
func withArgs(list []procs.Full) []procs.Full {
	out, err := runner.Output("ps", procs.PsAll("pid=,args=")...)
	if err != nil {
		return list
	}
	args := procs.ParseArgs(out)
	for i := range list {
		list[i].Args = args[list[i].PID]
	}
	return list
}

// hogLines: процессы, которые больше часа едят хотя бы целое ядро, строками для check и motd.
func hogLines(indent string) []string {
	list, err := allProcs()
	if err != nil {
		return nil
	}
	var out []string
	for _, h := range procs.Hogs(list, 100, time.Hour) {
		line := indent + ui.Warn(fmt.Sprintf("%s ест %s", h.Name, procs.Cores(h.CPU)))
		detail := "уже " + human(h.Uptime)
		if h.Note != "" {
			detail = h.Note + ", " + detail
		}
		out = append(out, line+ui.Dim("  "+detail+", pid "+h.PID+"  (tim kill "+h.PID+")"))
	}
	return out
}
