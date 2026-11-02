package servers

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/baseline"
	"github.com/TimurTurdyev/tim/internal/cache"
	"github.com/TimurTurdyev/tim/internal/paths"
	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
)

// exploreTimeout: nginx -T и счёт журналов за сутки на нагруженном сервере идут дольше обычного отчёта.
const exploreTimeout = 2 * time.Minute

func explorePath(target string) string {
	return cache.Path(paths.State("servers-explore"), hostKey(target))
}

// collectExplore: сайты и пользователи сервера по ssh; удачный обход - в кэш: из него новое на экране сервера
// и на главном объясняет себя.
func collectExplore(target string) (srvinfo.Explore, error) {
	out, err := runner.OutputStdinWithin(exploreTimeout, srvinfo.ExploreScript(), "ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=15", target, "sh -s")
	if !strings.Contains(out, "@@end") {
		if err == nil {
			err = fmt.Errorf("скрипт не дошёл до конца")
		}
		return srvinfo.Explore{}, err
	}
	e := srvinfo.ParseExplore(out, time.Now())
	if !runner.DryRun {
		_ = cache.Store(explorePath(target), e, 0o644)
	}
	return e, nil
}

// savedExplore: последний обход сайтов и пользователей.
func savedExplore(target string) (srvinfo.Explore, time.Time, bool) {
	e, ok := cache.Load[srvinfo.Explore](explorePath(target))
	return e.Value, e.Fetched, ok
}

// explained: откуда взялось новое, если это видно по обходу, - "FASTPANEL вместе с сайтом simple-talk.ru".
// Объяснённое не беда: на главном оно не висит и отмечать его не нужно.
func explained(target string, it baseline.Item) string {
	if it.Kind != "user" {
		return ""
	}
	e, _, ok := savedExplore(target)
	if !ok {
		return ""
	}
	name, _, _ := strings.Cut(it.Key, " ")
	for _, a := range e.Accounts {
		if a.Name != name || a.By == "" || a.UID == 0 {
			continue
		}
		if len(a.Sites) == 0 {
			return "заведён: " + a.By
		}
		return "заведён " + a.By + " вместе с сайтом " + strings.Join(a.Sites, ", ")
	}
	return ""
}

// unexplained: новое, которое обход не объяснил, - только оно бедой.
func unexplained(target string) []baseline.Item {
	var out []baseline.Item
	for _, it := range baseline.Pending(object(target)) {
		if explained(target, it) == "" {
			out = append(out, it)
		}
	}
	return out
}
