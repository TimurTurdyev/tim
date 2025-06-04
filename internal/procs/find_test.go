package procs

import "testing"

const psSample = `15033 83818   501   7360   0,0    10:43:14 S    /Applications/Google Chrome.app/Contents/Frameworks/Google Chrome Framework.framework/Versions/154.0.8037.98/Helpers/Google Chrome Helper.app/Contents/MacOS/Google Chrome Helper
83818     1   501  66976   12,5 12-01:40:39 S    /Applications/Google Chrome.app/Contents/MacOS/Google Chrome
    1     0     0  20912   0,1 17-03:28:21 Ss   /sbin/launchd
 4242  4100   501 102400   99,0    01:02:03 R    /opt/homebrew/bin/node
 4243  4100   501   2048   0,0    01:02:03 Z    node
  700     1   501  40960   0,0  2-00:00:00 S    /System/Library/CoreServices/WindowServer`

func TestParseFull(t *testing.T) {
	list := ParseFull(psSample)
	if len(list) != 6 {
		t.Fatalf("разобрано %d", len(list))
	}
	if p := list[1]; p.PID != "83818" || p.PPID != "1" || p.UID != 501 || p.CPU != 12.5 || p.Uptime.Hours() < 24*12 {
		t.Fatalf("%+v", p)
	}
	if list[4].State != "Z" {
		t.Fatalf("состояние: %+v", list[4])
	}
}

func TestFindTypoAndGrouping(t *testing.T) {
	list := ParseFull(psSample)
	got := Find(list, "goglehrome")
	if len(got) != 1 || got[0].Name != "Google Chrome" || len(got[0].Procs) != 2 || !got[0].IsApp {
		t.Fatalf("%+v", got)
	}
	// Главный - самый старший процесс приложения, а не помощник.
	if got[0].Main.PID != "83818" {
		t.Fatalf("главный %s", got[0].Main.PID)
	}
	if n := Find(list, "node"); len(n) != 1 || len(n[0].Procs) != 2 || n[0].IsApp {
		t.Fatalf("node: %+v", n)
	}
	if len(Find(list, "zzzz")) != 0 {
		t.Fatal("несуществующее нашлось")
	}
}

func TestProtected(t *testing.T) {
	list := ParseFull(psSample)
	for _, p := range list {
		want := p.PID == "1" || AppName(p.Comm) == "WindowServer"
		if Protected(p) != want {
			t.Errorf("%s (%s): %v", p.PID, AppName(p.Comm), Protected(p))
		}
	}
}

func TestFindExactWins(t *testing.T) {
	list := ParseFull(psSample + "\n 900 1 501 100 0,0 01:00 S /usr/libexec/launchdadditions")
	if got := Find(list, "launchd"); len(got) != 1 || got[0].Name != "launchd" {
		t.Fatalf("%+v", got)
	}
}

func TestFindAppFirst(t *testing.T) {
	list := ParseFull(psSample + `
 5000  1 501 900 0,0 6-00:00:00 S chrome-devtools-mcp
 5001  1 501 900 0,0 6-00:00:00 S npm exec chrome-devtools-mcp@latest`)
	got := Find(list, "chrome")
	if len(got) < 2 || got[0].Name != "Google Chrome" {
		t.Fatalf("первым должен быть браузер: %+v", got[0].Name)
	}
}

func TestFindByFileName(t *testing.T) {
	list := ParseFull(psSample + "\n 7000 1 501 4096 296,0 02:00:00 R /Users/x/sdk/qemu-system-aarch64-headless")
	list[len(list)-1].Args = "qemu -avd Test_Phone -no-window"
	list = append(list, ParseFull(" 7001 1 501 100 0,0 01:00 S /System/Library/Frameworks/QuickLook/com.apple.quicklook.ThumbnailsAgent")...)
	got := Find(list, "qemu")
	if len(got) != 1 || got[0].Name != "Android-эмулятор Test_Phone" {
		t.Fatalf("%+v", got)
	}
}
