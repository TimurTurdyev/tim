// Package schedule: ежедневный запуск команды через launchd на macOS и systemd на Linux.
package schedule

import (
	"fmt"
	"html"
	"sort"
	"strings"
)

// Job: что и когда запускать.
type Job struct {
	Label  string   // dev.tim.check
	Args   []string // полный путь к программе первым
	Hour   int
	Minute int
	Every  int  // секунды; если задано, запуск по интервалу, а не в Hour:Minute
	Daemon bool // работает постоянно и перезапускается после падения
	Log    string
	Env    map[string]string
}

// ParseAt разбирает время вида 09:30.
func ParseAt(s string) (hour, minute int, err error) {
	if _, err = fmt.Sscanf(s, "%d:%d", &hour, &minute); err != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("время в виде 09:30: %s", s)
	}
	return hour, minute, nil
}

func esc(s string) string { return html.EscapeString(s) }

// Plist: агент launchd. StartCalendarInterval запускает и после сна, если время было пропущено.
func (j Job) Plist() string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + esc(j.Label) + `</string>
	<key>ProgramArguments</key>
	<array>
`)
	for _, a := range j.Args {
		b.WriteString("\t\t<string>" + esc(a) + "</string>\n")
	}
	b.WriteString("\t</array>\n")
	switch {
	case j.Daemon:
		b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n\t<key>KeepAlive</key>\n\t<true/>\n")
	case j.Every > 0:
		fmt.Fprintf(&b, "\t<key>StartInterval</key>\n\t<integer>%d</integer>\n", j.Every)
	default:
		fmt.Fprintf(&b, "\t<key>StartCalendarInterval</key>\n\t<dict>\n\t\t<key>Hour</key>\n\t\t<integer>%d</integer>\n\t\t<key>Minute</key>\n\t\t<integer>%d</integer>\n\t</dict>\n", j.Hour, j.Minute)
	}
	if len(j.Env) > 0 {
		b.WriteString("\t<key>EnvironmentVariables</key>\n\t<dict>\n")
		for _, k := range sortedKeys(j.Env) {
			b.WriteString("\t\t<key>" + esc(k) + "</key>\n\t\t<string>" + esc(j.Env[k]) + "</string>\n")
		}
		b.WriteString("\t</dict>\n")
	}
	if j.Log != "" {
		b.WriteString("\t<key>StandardOutPath</key>\n\t<string>" + esc(j.Log) + "</string>\n")
		b.WriteString("\t<key>StandardErrorPath</key>\n\t<string>" + esc(j.Log) + "</string>\n")
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// Systemd: пара service и timer для systemd --user. Persistent догоняет пропущенный запуск.
func (j Job) Systemd() (service, timer string) {
	quoted := make([]string, len(j.Args))
	for i, a := range j.Args {
		quoted[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	var env strings.Builder
	for _, k := range sortedKeys(j.Env) {
		env.WriteString(`Environment="` + k + "=" + j.Env[k] + "\"\n")
	}
	if j.Daemon {
		// Постоянная служба: таймер не нужен, после падения systemd поднимет её сам.
		service = "[Unit]\nDescription=" + j.Label + "\nAfter=network-online.target\nWants=network-online.target\n\n" +
			"[Service]\nType=simple\n" + env.String() + "ExecStart=" + strings.Join(quoted, " ") +
			"\nRestart=always\nRestartSec=5\n\n[Install]\nWantedBy=multi-user.target\n"
		return service, ""
	}
	service = "[Unit]\nDescription=" + j.Label + "\n\n[Service]\nType=oneshot\n" + env.String() +
		"ExecStart=" + strings.Join(quoted, " ") + "\n"
	when := fmt.Sprintf("OnCalendar=*-*-* %02d:%02d:00\nPersistent=true", j.Hour, j.Minute)
	if j.Every > 0 {
		when = fmt.Sprintf("OnBootSec=5min\nOnUnitActiveSec=%ds", j.Every)
	}
	timer = fmt.Sprintf("[Unit]\nDescription=%s\n\n[Timer]\n%s\n\n[Install]\nWantedBy=timers.target\n", j.Label, when)
	return service, timer
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
