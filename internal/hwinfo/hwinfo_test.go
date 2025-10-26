package hwinfo

import (
	"os"
	"testing"
)

func read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestParseDisplays(t *testing.T) {
	g := ParseDisplays([]byte(read(t, "displays.json")))
	if len(g) != 1 || g[0].Model != "Apple M5 Pro" || g[0].Cores != 20 || g[0].Metal != "Metal 4" {
		t.Fatalf("%+v", g)
	}
	d := g[0].Displays
	if len(d) != 1 || !d[0].Main || !d[0].Internal || d[0].Pixels != "3456 x 2234" {
		t.Fatalf("%+v", d)
	}
}

func TestParseGPULoad(t *testing.T) {
	g := ParseGPULoad(read(t, "ioreg-accel.txt"))
	if !g.Valid || g.Device != 100 || g.Renderer != 100 || g.InUse < 1<<30 {
		t.Fatalf("%+v", g)
	}
	if ParseGPULoad("ничего").Valid {
		t.Fatal("без статистики - не валидно")
	}
}

func TestParseGPUClients(t *testing.T) {
	c := ParseGPUClients(read(t, "ioreg-clients.txt"))
	if len(c) == 0 || c[0].PID == "" || c[0].Name == "" {
		t.Fatalf("%+v", c)
	}
	// Один процесс с двумя клиентами - одна строка.
	dup := ParseGPUClients(`"IOUserClientCreator" = "pid 615, WindowServer"` + "\n" + `"IOUserClientCreator" = "pid 615, WindowServer"`)
	if len(dup) != 1 {
		t.Fatalf("%+v", dup)
	}
}

func TestParseSwapAndPressure(t *testing.T) {
	s, ok := ParseSwap("total = 7168,00M  used = 5625,25M  free = 1542,75M  (encrypted)")
	if !ok || s.Total != 7168<<20 || s.Used/(1<<20) != 5625 {
		t.Fatalf("%+v", s)
	}
	if n, ok := ParseFreePercent("The system has 25769803776 (1572864 pages with a page size of 16384).\n...\nSystem-wide memory free percentage: 37%\n"); !ok || n != 37 {
		t.Fatalf("%d %v", n, ok)
	}
}

func TestParseTherm(t *testing.T) {
	if w := ParseTherm(read(t, "therm.txt")); w != "" {
		t.Fatalf("ограничений нет, а получили %q", w)
	}
	if w := ParseTherm("CPU Power notify\n\tCPU_Scheduler_Limit \t= 100\n\tCPU_Available_CPUs \t= 18\n\tCPU_Speed_Limit \t= 70\n"); w != "процессор ограничен до 70%" {
		t.Fatalf("%q", w)
	}
}

func TestParseDiskutilAndIostat(t *testing.T) {
	d := ParseDiskutil(read(t, "diskutil.txt"))
	if !d.SSD || d.SMART != "Verified" || d.Protocol != "Apple Fabric" {
		t.Fatalf("%+v", d)
	}
	if n, ok := ParseIostatCPU(read(t, "iostat.txt")); !ok || n != 5 {
		t.Fatalf("%d %v", n, ok)
	}
}

func TestLinuxSources(t *testing.T) {
	a, ok1 := ParseProcStat("cpu  100 0 100 700 100 0 0 0 0 0\ncpu0 1 2 3\n")
	b, ok2 := ParseProcStat("cpu  200 0 200 750 150 0 0 0 0 0\n")
	if !ok1 || !ok2 || Busy(a, b) != 66 {
		t.Errorf("busy %d (%+v %+v)", Busy(a, b), a, b)
	}
	if sw, ok := ParseMeminfoSwap("SwapTotal:  2097148 kB\nSwapFree:  1048574 kB\n"); !ok || sw.Used != 1048574*1024 {
		t.Errorf("swap %+v", sw)
	}
	g := ParseNvidiaSmi("NVIDIA GeForce RTX 4090, 87, 20480, 24564\n")
	if len(g) != 1 || g[0].Util != 87 || g[0].MemTotal != 24564<<20 {
		t.Errorf("nvidia %+v", g)
	}
}
