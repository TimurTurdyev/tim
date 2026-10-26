package servers

import (
	"time"

	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
)

// Кэш состояния серверов: список Серверов и ВНИМАНИЕ на главном открываются из него, не заходя
// по ssh на каждый алиас; ssh - по enter или r.

func savedPath(target string) string { return cache.Path(paths.State("servers"), hostKey(target)) }

// save запоминает состояние доступного сервера; недоступный не затирает последнее известное.
func save(s serverState) {
	if runner.DryRun || s.Err != "" {
		return
	}
	_ = cache.Store(savedPath(s.Target), s, 0o644)
}

// saved: последнее известное состояние сервера и когда его сняли.
func saved(target string) (serverState, time.Time, bool) {
	e, ok := cache.Load[serverState](savedPath(target))
	return e.Value, e.Fetched, ok
}

// collect: состояние сервера по ssh, удачное - в кэш.
func collect(t Server) serverState {
	s := serverState{Target: t.Alias, Projects: t.Projects}
	r, err := srvinfoCollect(t.Alias)
	if err != nil {
		s.Err = sshError(err)
		return s
	}
	s.Report, s.Findings = r, evaluate(t.Alias, r)
	s.Remembered = observe(t.Alias, r)
	save(s)
	return s
}

// srvinfoCollect: отчёт сервера по ssh с общим сроком.
func srvinfoCollect(target string) (srvinfo.Report, error) {
	return collectFull(target)
}
