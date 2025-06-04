package procs

import (
	"testing"
	"time"
)

func TestDescribe(t *testing.T) {
	cases := []struct {
		comm, args, name, note string
	}{
		{"/Users/x/Library/Android/sdk/emulator/qemu/darwin-aarch64/qemu-system-aarch64-headless",
			"/Users/x/.../qemu-system-aarch64-headless -avd Medium_Phone_API_36 -no-snapshot-save -no-window",
			"Android-эмулятор Medium_Phone_API_36", "без окна"},
		{"/usr/local/bin/qemu-system-x86_64", "qemu-system-x86_64 -m 2048", "виртуальная машина qemu", ""},
		{"/usr/bin/java", "java -Xmx2g -cp gradle-launcher.jar org.gradle.launcher.daemon.bootstrap.GradleDaemon 8.10", "демон Gradle", "живёт после сборки"},
		{"/System/Library/.../mds_stores", "/System/.../mds_stores", "Spotlight", "индексирует диск"},
	}
	for _, c := range cases {
		name, note, ok := Describe(c.comm, c.args)
		if !ok || name != c.name || note != c.note {
			t.Errorf("%s: %q %q %v", c.comm, name, note, ok)
		}
	}
	if _, _, ok := Describe("/Applications/Firefox.app/Contents/MacOS/firefox", "firefox"); ok {
		t.Error("обычное приложение описывать не нужно")
	}
	if n := Name("/opt/homebrew/bin/node", "node server.js"); n != "node" {
		t.Errorf("обычный процесс: %q", n)
	}
}

func TestHogs(t *testing.T) {
	list := []Full{
		{Sample: Sample{PID: "101", CPU: 489, Comm: "qemu-system-aarch64-headless", Args: "qemu -avd Pixel -no-window"}, UID: 501, Uptime: 76 * time.Hour},
		{Sample: Sample{PID: "2", CPU: 300, Comm: "node"}, UID: 501, Uptime: 5 * time.Minute},      // свежий - ещё работа
		{Sample: Sample{PID: "3", CPU: 40, Comm: "node"}, UID: 501, Uptime: 10 * time.Hour},        // мало
		{Sample: Sample{PID: "4", CPU: 250, Comm: "kernel_task"}, UID: 0, Uptime: 100 * time.Hour}, // системный
		{Sample: Sample{PID: "5", CPU: 120, Comm: "/opt/homebrew/bin/php"}, UID: 501, Uptime: 2 * time.Hour},
	}
	got := Hogs(list, 100, time.Hour)
	if len(got) != 2 || got[0].Name != "Android-эмулятор Pixel" || got[0].Note != "без окна" || got[1].Name != "php" {
		t.Fatalf("%+v", got)
	}
}

func TestCores(t *testing.T) {
	cases := map[float64]string{100: "ядро", 210: "2 ядра", 489: "почти 5 ядер", 500: "5 ядер", 1100: "11 ядер", 2150: "22 ядра"}
	for cpu, want := range cases {
		if got := Cores(cpu); got != want {
			t.Errorf("%v: %q, ожидали %q", cpu, got, want)
		}
	}
}
