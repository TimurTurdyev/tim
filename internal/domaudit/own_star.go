package domaudit

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/sweep"
)

const (
	poolBudget  = 5 * time.Second
	poolRounds  = 3   // уровней вложенности, на которых ищем запись *
	maxParents  = 100 // родителей на уровень
	retryBudget = 5 * time.Second
	maxRetry    = 2000 // молчит больше имён - путь не работает, повтор не поможет
)

// pool: что зона отвечает на несуществующее имя под родителем. asked без known - спросили, зона промолчала: о
// записи * под ним ничего не известно.
type pool struct {
	ips, targets []string
	nodata       bool
	asked, known bool
}

// covers: ответ неотличим от ответа на несуществующее имя.
func (p pool) covers(a sweep.Answer) bool {
	switch a.Kind {
	case sweep.CNAME:
		return slices.Contains(p.targets, a.Target)
	case sweep.NoData:
		return p.nodata
	}
	return len(p.ips) > 0 && !slices.ContainsFunc(a.IPs, func(ip string) bool { return !slices.Contains(p.ips, ip) })
}

// poolOf: ответ зоны на несуществующее имя рядом с name - у родителя, а если тот не ответил, у ближайшего
// ответившего предка: запись * действует и глубже, пока там нет своего узла. direct - ответил сам родитель.
func (c *collector) poolOf(name string) (p pool, direct, known bool) {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	parent := parentOf(name)
	for at := parent; at != ""; at = parentOf(at) {
		if got := c.pool[at]; got.known {
			return got, at == parent, true
		}
		if at == c.domain {
			break
		}
	}
	return pool{}, false, false
}

// starred: под родителем зона отвечает адресом на любое имя.
func (c *collector) starred(parent string) bool {
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	p := c.pool[parent]
	return len(p.ips) > 0 || len(p.targets) > 0
}

// star спрашивает зону о записи * под родителями ответивших имён: сначала ближние к вершине, глубже - только там,
// где ответ не объяснила запись * выше. В зоне с * под вершиной это одна проба, а не проба на каждого родителя.
func (c *collector) star(via dnsPath, hits []sweep.Answer, budget time.Duration) {
	for range poolRounds {
		need := c.unasked(hits)
		if len(need) == 0 {
			return
		}
		c.askPools(via, need, budget)
	}
}

// unasked: родители ответивших имён, которых ещё не спрашивали и чей ответ не объяснён записью * выше, - самые
// близкие к вершине, с наибольшим числом имён первыми.
func (c *collector) unasked(hits []sweep.Answer) []string {
	count := map[string]int{}
	for _, a := range hits {
		if p, direct, known := c.poolOf(a.Name); !direct && (!known || !p.covers(a)) {
			count[parentOf(a.Name)]++
		}
	}
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	depth := -1
	var out []string
	for parent := range count {
		if c.pool[parent].asked {
			continue
		}
		switch d := strings.Count(parent, "."); {
		case depth < 0 || d < depth:
			depth, out = d, []string{parent}
		case d == depth:
			out = append(out, parent)
		}
	}
	slices.SortFunc(out, func(a, b string) int {
		if d := count[b] - count[a]; d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	return out[:min(len(out), maxParents)]
}

// askPools: несколько несуществующих имён под каждым родителем - у балансировщика каждый ответ свой.
func (c *collector) askPools(via dnsPath, parents []string, budget time.Duration) {
	nonce := "tim-nx-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	var names []string
	for _, p := range parents {
		for i := range wildTries {
			names = append(names, nonce+strconv.Itoa(i)+"."+p)
		}
	}
	opt := via.opt
	opt.Budget = budget
	got := map[string]pool{}
	for i, a := range sweep.Run(context.Background(), names, opt).Answers {
		parent := parentOf(names[i])
		p := got[parent]
		switch a.Kind {
		case sweep.Addr:
			p.ips, p.known = append(p.ips, a.IPs...), true
		case sweep.CNAME:
			p.targets, p.known = append(p.targets, a.Target), true
		case sweep.NoData:
			p.nodata, p.known = true, true
		case sweep.NX, sweep.Refer:
			p.known = true
		}
		got[parent] = p
	}
	c.poolMu.Lock()
	defer c.poolMu.Unlock()
	for _, parent := range parents {
		p := got[parent]
		p.asked = true
		c.pool[parent] = p
	}
}

// explicit: какие из имён, неотличимых от записи *, заведены явно: под таким именем запись * уже не действует, и на
// несуществующее имя ниже зона отвечает "нет". all=false - проверить успели не все.
func explicit(via dnsPath, blank []string, budget time.Duration) (firm []string, all bool) {
	if len(blank) == 0 {
		return nil, true
	}
	probes := make([]string, len(blank))
	for i, n := range blank {
		probes[i] = "tim-nx." + n
	}
	answers, _ := askNames(via, probes, budget)
	all = len(answers) == len(blank)
	for i, a := range answers {
		switch a.Kind {
		case sweep.NX:
			firm = append(firm, blank[i])
		case sweep.None, sweep.Fail:
			all = false
		}
	}
	return firm, all
}

// askNames: ответы на имена по порядку; молчавшие спрашиваются ещё раз - потерянный пакет не должен значить "не проверено".
func askNames(via dnsPath, names []string, budget time.Duration) (answers []sweep.Answer, limited bool) {
	opt := via.opt
	opt.Budget = budget
	res := sweep.Run(context.Background(), names, opt)
	var again []string
	var at []int
	for i, a := range res.Answers {
		if a.Kind == sweep.None || a.Kind == sweep.Fail {
			again, at = append(again, names[i]), append(at, i)
		}
	}
	if len(again) == 0 || len(again) > maxRetry {
		return res.Answers, res.Limited
	}
	opt.Budget = min(retryBudget, budget)
	for j, a := range sweep.Run(context.Background(), again, opt).Answers {
		res.Answers[at[j]] = a
	}
	return res.Answers, res.Limited
}
