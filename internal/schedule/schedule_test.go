package schedule

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestParseAt(t *testing.T) {
	if h, m, err := ParseAt("09:30"); err != nil || h != 9 || m != 30 {
		t.Fatalf("%d %d %v", h, m, err)
	}
	for _, bad := range []string{"25:00", "9", "ab:cd", "10:60"} {
		if _, _, err := ParseAt(bad); err == nil {
			t.Errorf("%q принят", bad)
		}
	}
}

func TestPlistIsValidXML(t *testing.T) {
	j := Job{Label: "dev.tim.check", Args: []string{"/Users/a b/go/bin/tim", "check", "--notify"},
		Hour: 9, Minute: 5, Log: "/tmp/x&y.log", Env: map[string]string{"PATH": "/usr/bin:/bin", "TIM_NO_INPUT": "1"}}
	p := j.Plist()
	// Амперсанд в пути не должен ломать XML: иначе launchctl молча откажется грузить агента.
	dec := xml.NewDecoder(strings.NewReader(p))
	dec.Strict = true
	dec.Entity = xml.HTMLEntity
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() != "EOF" {
				t.Fatalf("plist не XML: %v\n%s", err, p)
			}
			break
		}
	}
	for _, want := range []string{"<string>/Users/a b/go/bin/tim</string>", "<integer>9</integer>", "<integer>5</integer>", "x&amp;y.log", "<key>TIM_NO_INPUT</key>"} {
		if !strings.Contains(p, want) {
			t.Errorf("нет %q", want)
		}
	}
}

func TestSystemd(t *testing.T) {
	j := Job{Label: "tim check", Args: []string{"/usr/local/bin/tim", "check"}, Hour: 7, Minute: 0}
	svc, timer := j.Systemd()
	if !strings.Contains(svc, `ExecStart="/usr/local/bin/tim" "check"`) || !strings.Contains(timer, "OnCalendar=*-*-* 07:00:00") {
		t.Fatalf("%s\n%s", svc, timer)
	}
}
