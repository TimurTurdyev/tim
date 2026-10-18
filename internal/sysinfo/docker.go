package sysinfo

import (
	"strconv"
	"strings"

	"github.com/TimurTurdyev/tim/internal/runner"
)

// Container: контейнер, который пробросил порт на машину.
type Container struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}

// Containers: проброшенные порты машины -> контейнер. Без docker или с остановленным движком - пусто:
// docker ps на остановленном движке виснет, поэтому сначала DockerUp.
func Containers() map[int]Container {
	if !runner.Has("docker") || !DockerUp() {
		return nil
	}
	out, err := runner.Output("docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Ports}}")
	if err != nil {
		return nil
	}
	return ParseDockerPorts(out)
}

// ParseDockerPorts разбирает docker ps --format "{{.Names}}\t{{.Image}}\t{{.Ports}}". Порты вида
// 0.0.0.0:5432->5432/tcp, :::5432->5432/tcp и диапазоны 0.0.0.0:8000-8001->8000-8001/tcp; порт без ->
// наружу не проброшен.
func ParseDockerPorts(out string) map[int]Container {
	m := map[int]Container{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 3 {
			continue
		}
		c := Container{Name: f[0], Image: f[1]}
		for _, item := range strings.Split(f[2], ",") {
			host, _, ok := strings.Cut(strings.TrimSpace(item), "->")
			if !ok {
				continue
			}
			host = host[strings.LastIndex(host, ":")+1:]
			from, to, isRange := strings.Cut(host, "-")
			lo, err1 := strconv.Atoi(from)
			hi, err2 := strconv.Atoi(to)
			if !isRange {
				hi, err2 = lo, err1
			}
			if err1 != nil || err2 != nil || hi < lo || hi-lo > 1000 {
				continue
			}
			for p := lo; p <= hi; p++ {
				m[p] = c
			}
		}
	}
	return m
}
