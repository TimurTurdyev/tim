package audit

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"time"

	"github.com/TimurTurdyev/tim/internal/level"
	"github.com/TimurTurdyev/tim/internal/par"
)

// DBPorts: что не должно отвечать из интернета. 2375 - Docker API без TLS, это root на машине.
// Знают и аудит снаружи, и отчёт о сервере по ssh.
var DBPorts = map[int]string{
	3306: "MySQL", 5432: "PostgreSQL", 6379: "Redis", 27017: "MongoDB", 9200: "Elasticsearch",
	11211: "Memcached", 5984: "CouchDB", 8086: "InfluxDB", 2375: "Docker API без TLS", 2376: "Docker API",
}

// PortFindings: порты баз, которые отвечают из интернета.
func PortFindings(open []int) []Finding {
	var out []Finding
	for _, p := range open {
		out = append(out, Finding{Area: "порты", Level: level.Bad, Text: fmt.Sprintf("наружу отвечает %s (%d)", DBPorts[p], p),
			Why: "базу перебирают и ломают из интернета; Docker API без TLS - это root на сервере",
			Fix: "слушать на 127.0.0.1, снаружи ходить через ssh-туннель: tim tunnel db"})
	}
	return out
}

// openPorts: какие порты баз отвечают на адресе сайта.
func openPorts(host string) []int {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return nil
	}
	ip := ips[0]
	for _, x := range ips {
		if x.To4() != nil {
			ip = x
			break
		}
	}
	var ports []int
	for p := range DBPorts {
		ports = append(ports, p)
	}
	slices.Sort(ports)
	alive := par.Map(ports, len(ports), func(p int) bool {
		c, err := net.DialTimeout("tcp", net.JoinHostPort(ip.String(), strconv.Itoa(p)), 2*time.Second)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	})
	var open []int
	for i, ok := range alive {
		if ok {
			open = append(open, ports[i])
		}
	}
	return open
}
