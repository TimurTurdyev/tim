// Package srvinfo: состояние Linux-сервера одним заходом по ssh. Скрипт только читает;
// всё, что требует root, идёт через sudo -n, который без пароля не спрашивает, а отказывает.
package srvinfo

// Script печатает разделы с маркерами @@имя; разбор на нашей стороне.
const Script = `export LC_ALL=C
echo @@os; grep -E '^(ID|VERSION_ID|PRETTY_NAME)=' /etc/os-release 2>/dev/null
echo @@uptime; cat /proc/uptime 2>/dev/null; nproc 2>/dev/null; cat /proc/loadavg 2>/dev/null
echo @@mem; grep -E '^(MemTotal|MemAvailable|SwapTotal|SwapFree):' /proc/meminfo 2>/dev/null
echo @@df; df -kP -x tmpfs -x devtmpfs -x squashfs -x overlay -x efivarfs 2>/dev/null
echo @@inodes; df -iP -x tmpfs -x devtmpfs -x squashfs -x overlay -x efivarfs 2>/dev/null
echo @@failed; systemctl --failed --no-legend --plain 2>/dev/null
echo @@reboot; if [ -f /var/run/reboot-required ]; then echo yes; cat /var/run/reboot-required.pkgs 2>/dev/null; fi
echo @@updates; if command -v apt >/dev/null 2>&1; then apt list --upgradable 2>/dev/null | grep -- '-security' | cut -d/ -f1; else echo noapt; fi
echo @@ports; ss -tlnH 2>/dev/null
echo @@sshd; sudo -n sshd -T 2>/dev/null | grep -Ei '^(passwordauthentication|permitrootlogin) ' || echo nosudo
echo @@firewall; sudo -n ufw status 2>/dev/null | head -1; echo "fail2ban $(systemctl is-active fail2ban 2>/dev/null)"
echo @@nginx; if command -v nginx >/dev/null 2>&1; then sudo -n nginx -t 2>&1 | tail -1 || echo nosudo; fi
echo @@certs; sudo -n sh -c 'for f in /etc/letsencrypt/live/*/cert.pem; do [ -f "$f" ] && echo "$f $(openssl x509 -enddate -noout -in "$f")"; done' 2>/dev/null
echo @@end
`
