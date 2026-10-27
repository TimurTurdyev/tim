package srvinfo

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Collect: один заход по ssh за отчётом; скрипт уходит через stdin, на сервере ничего не остаётся.
// timeout - на соединение; сам скрипт (apt, nginx -t через sudo) на слабом VPS идёт дольше.
func Collect(target string, timeout time.Duration) (Report, error) {
	out, err := remote(target, Script, timeout, time.Minute)
	if err != nil {
		return Report{}, err
	}
	return Parse(out), nil
}

// CollectTraffic: хвосты журналов запросов за окно since; на нагруженном сервере журналы большие,
// предел шире обычного.
func CollectTraffic(target string, since time.Duration) (Traffic, error) {
	out, err := remote(target, TrafficScript(), 15*time.Second, 2*time.Minute)
	if err != nil {
		return Traffic{}, err
	}
	return ParseTraffic(out, time.Now(), since), nil
}

// remote: скрипт на сервере через sh -s; connect - сколько ждать соединение, limit - весь скрипт.
// Без маркера конца вывод считается обрывом, даже если ssh вышел с нулём.
func remote(target, script string, connect, limit time.Duration) (string, error) {
	out, err := runner.OutputStdinWithin(limit, script, "ssh", "-o", "BatchMode=yes",
		"-o", "ConnectTimeout="+fmt.Sprint(int(connect.Seconds())), target, "sh -s")
	if !strings.Contains(out, "@@end") {
		if err == nil {
			err = fmt.Errorf("скрипт не дошёл до конца")
		}
		return out, err
	}
	return out, nil
}

// Root: корневой раздел - по нему место в сводках и прогнозе.
func (r Report) Root() (Disk, bool) {
	for _, d := range r.Disks {
		if d.Mount == "/" {
			return d, true
		}
	}
	return Disk{}, false
}
