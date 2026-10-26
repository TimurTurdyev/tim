// Package srvinfo: состояние Linux-сервера одним заходом по ssh. Скрипт только читает;
// всё, что требует root, идёт через sudo -n, который без пароля не спрашивает, а отказывает.
package srvinfo

// watchScript: то, что сравнивается с прошлым разом (ключи, пользователи, юниты, таймеры, крон),
// признаки взлома, службы со старыми библиотеками и порты Docker. Чужое читается через sudo -n.
const watchScript = `S=; sudo -n true 2>/dev/null && S="sudo -n"
echo @@keys; awk -F: '($3==0 || $3>=1000) && $7 !~ /(nologin|false|sync)$/ {print $1, $6}' /etc/passwd | while read -r u h; do f="$h/.ssh/authorized_keys"
  if [ -r "$f" ]; then ssh-keygen -lf "$f" 2>/dev/null | sed "s|^|key $u |"; echo "read $u"
  elif [ -n "$S" ]; then $S test -e "$f" && $S ssh-keygen -lf "$f" 2>/dev/null | sed "s|^|key $u |"; echo "read $u"
  elif [ -x "$h" ] && { [ ! -e "$h/.ssh" ] || { [ -x "$h/.ssh" ] && [ ! -e "$f" ]; }; }; then echo "read $u"
  else echo "unread $u"; fi
done
echo @@users; awk -F: '($3==0 || $3>=1000) && $7 !~ /(nologin|false|sync)$/ {print $1, $3, $7}' /etc/passwd
echo @@enabled; systemctl list-unit-files --type=service --state=enabled --no-legend --plain 2>/dev/null | awk '{print $1}'
echo @@timers; systemctl list-timers --all --no-legend --plain 2>/dev/null | awk '{for(i=1;i<=NF;i++) if ($i ~ /\.timer$/) {print $i; break}}'
echo @@cron; for f in /etc/crontab /etc/cron.d/*; do [ -f "$f" ] && grep -vE '^[[:space:]]*(#|$)|^[A-Z_]+=' "$f" | sed "s|^|$f: |"; done
crontab -l 2>/dev/null | grep -vE '^[[:space:]]*(#|$)' | sed "s|^|crontab $(id -un): |"
if [ -n "$S" ]; then echo @@cronspool; $S sh -c 'for f in /var/spool/cron/crontabs/* /var/spool/cron/*; do [ -f "$f" ] && grep -vE "^[[:space:]]*(#|$)" "$f" | sed "s|^|$f: |"; done' 2>/dev/null; fi
echo @@suspect
$S sh -c 'for p in /proc/[0-9]*; do e=$(readlink "$p/exe" 2>/dev/null) || continue; case "$e" in /tmp/*|/dev/shm/*|/var/tmp/*|/memfd:*|*" (deleted)") echo "exe ${p#/proc/} $(cat "$p/comm" 2>/dev/null) $e";; esac; done' 2>/dev/null
[ -s /etc/ld.so.preload ] && sed 's/^/preload /' /etc/ld.so.preload
ps -eo pid=,pcpu=,comm=,args= 2>/dev/null | grep -iE 'stratum\+(tcp|ssl)|xmrig|kdevtmpfsi|kinsing|minerd|cpuminer' | grep -v grep | sed 's/^/miner /'
$S grep -lE '(curl|wget)[^|#]*[|][[:space:]]*(ba|da)?sh|base64[[:space:]]+(-d|--decode)' /etc/crontab /etc/cron.d/* /var/spool/cron/crontabs/* /etc/systemd/system/*.service 2>/dev/null | sed 's/^/fetchsh /'
$S ss -tlnpH 2>/dev/null | grep -E '[(]"(sh|bash|dash|zsh|nc|ncat|netcat|socat)",' | sed 's/^/shellport /'
echo @@stale; if [ -n "$S" ] || [ "$(id -u)" = 0 ]; then $S sh -c 'for p in /proc/[0-9]*; do grep -qE "[.]so[.0-9]* [(]deleted[)]" "$p/maps" 2>/dev/null && cat "$p/comm"; done' 2>/dev/null | sort -u; else echo nosudo; fi
echo @@docker; if command -v docker >/dev/null 2>&1; then D=docker; docker info >/dev/null 2>&1 || D="$S docker"; $D ps --format '{{.Names}}|{{.Ports}}' 2>/dev/null; fi
`

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
echo @@sshd; ` + protSSHD + `
echo @@firewall; sudo -n ufw status 2>/dev/null | head -1; ` + protGuards + `
echo @@host; echo "virt $(systemd-detect-virt 2>/dev/null)"; [ -d /etc/netplan ] && echo netplan
echo @@nginx; if command -v nginx >/dev/null 2>&1; then sudo -n nginx -t 2>&1 | tail -1 || echo nosudo; fi
echo @@certs; sudo -n sh -c 'for f in /etc/letsencrypt/live/*/cert.pem; do [ -f "$f" ] && echo "$f $(openssl x509 -enddate -noout -in "$f")"; done' 2>/dev/null
` + watchScript + `echo @@end
`
