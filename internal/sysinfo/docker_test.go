package sysinfo

import "testing"

func TestParseDockerPorts(t *testing.T) {
	out := "shop-db-1\tpostgres:16\t0.0.0.0:5432->5432/tcp, :::5432->5432/tcp\n" +
		"shop-app-1\tshop:dev\t127.0.0.1:8000-8001->8000-8001/tcp, 9000/tcp\n" +
		"worker\tredis:7\t6379/tcp\n"
	m := ParseDockerPorts(out)
	if len(m) != 3 || m[5432].Name != "shop-db-1" || m[8001].Image != "shop:dev" {
		t.Fatalf("%+v", m)
	}
	if _, ok := m[6379]; ok {
		t.Error("непроброшенный порт")
	}
}
