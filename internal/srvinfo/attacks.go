package srvinfo

import (
	"fmt"
	"strings"
	"time"

	"github.com/TimurTurdyev/tim/internal/audit"
)

// Защита от перебора и настройки входа - общие куски отчёта о сервере и отчёта по журналам.
const (
	protGuards = `echo "fail2ban $(systemctl is-active fail2ban 2>/dev/null)"; echo "crowdsec $(systemctl is-active crowdsec 2>/dev/null)"`
	protSSHD   = `sudo -n sshd -T 2>/dev/null | grep -Ei '^(passwordauthentication|kbdinteractiveauthentication|permitrootlogin) ' || echo nosudo`
)

// attacksScript читает журналы за окно SINCE (секунды) и сворачивает счёт на сервере: за сутки перебора
// бывают десятки тысяч строк, по ssh едут только итоги. Журнал ssh читается один раз во временный файл,
// который стирается на выходе. Только чтение; где нужен root - sudo -n, который без пароля отказывает.
const attacksScript = `export LC_ALL=C
T=$(mktemp /tmp/tim-attacks.XXXXXX) || exit 1
trap 'rm -f "$T"' EXIT
J() { journalctl -q --no-pager -o short-iso --since "-${SINCE}s" "$@" 2>/dev/null; }
SJ() { sudo -n journalctl -q --no-pager -o short-iso --since "-${SINCE}s" "$@" 2>/dev/null; }
if [ -n "$(J -u ssh -u sshd -n 1)" ]; then J -u ssh -u sshd > "$T"; SRC=journal
elif [ -n "$(SJ -u ssh -u sshd -n 1)" ]; then SJ -u ssh -u sshd > "$T"; SRC=journal-sudo
elif [ -r /var/log/auth.log ]; then cat /var/log/auth.log > "$T"; SRC=authlog
elif sudo -n test -r /var/log/auth.log 2>/dev/null; then sudo -n cat /var/log/auth.log > "$T"; SRC=authlog-sudo
else SRC=none; fi
FAIL='Failed password|Invalid user|maximum authentication attempts'
echo @@sshsrc; echo "$SRC"
if [ "$SRC" != none ]; then
  echo @@sshfail; grep -E "$FAIL" "$T" | grep -oE 'from [0-9a-fA-F.:]+' | awk '{print $2}' | sort | uniq -c | sort -rn | awk 'NR<=50{print} END{print "total", NR}'
  echo @@sshfailtotal; grep -cE "$FAIL" "$T"
  echo @@sshusers; sed -nE 's/.*(Invalid user |Failed password for (invalid user )?)([^ ]+) from .*/\3/p' "$T" | sort | uniq -c | sort -rn | head -10
  echo @@sshok; grep -E 'Accepted (password|publickey|keyboard-interactive)' "$T" | tail -n 200
fi
echo @@probes
P='__PROBES__'
` + logLoop + `  echo "file $f"
  $R "$f" | grep -E "\"[A-Z]+ [^\"]*($P)" | tail -n 3000 | sed "s|^|$f |"
done
echo @@ufw
if [ -n "$(J -k -b all -n 1)" ]; then J -k -b all > "$T"; elif [ -n "$(SJ -k -b all -n 1)" ]; then SJ -k -b all > "$T"; elif [ -r /var/log/ufw.log ]; then cat /var/log/ufw.log > "$T"; else : > "$T"; echo none; fi
if [ -s "$T" ]; then
  echo "total $(grep -c 'UFW BLOCK' "$T")"
  grep 'UFW BLOCK' "$T" | grep -oE 'SRC=[^ ]+' | cut -d= -f2 | sort | uniq -c | sort -rn | head -30 | sed 's/^/src /'
  grep 'UFW BLOCK' "$T" | grep -oE 'DPT=[0-9]+' | cut -d= -f2 | sort | uniq -c | sort -rn | head -15 | sed 's/^/dpt /'
fi
echo @@f2b
if ! command -v fail2ban-client >/dev/null 2>&1; then echo none
elif ! sudo -n fail2ban-client status >/dev/null 2>&1; then echo nosudo
else for j in $(sudo -n fail2ban-client status | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do echo "jail $j"; sudo -n fail2ban-client status "$j" | grep -E 'Currently banned|Total banned|Banned IP list'; done; fi
echo @@cscli
if ! command -v cscli >/dev/null 2>&1; then echo none
elif ! out=$(sudo -n cscli decisions list -o json 2>/dev/null); then echo nosudo
else echo "$out"; fi
echo @@prot
` + protGuards + `
` + protSSHD + `
echo @@end
`

// logLoop: обход журналов запросов - из конфигов nginx и apache, стандартные пути и папка пользователя,
// куда их кладут панели. Читается хвост в __LINES__ строк, чужие журналы - через sudo -n.
const logLoop = `( grep -rhoE '^[[:space:]]*access_log[[:space:]]+[^ ;]+' /etc/nginx 2>/dev/null | awk '{print $2}'; grep -rhoE '^[[:space:]]*CustomLog[[:space:]]+"?[^ "]+' /etc/apache2 /etc/httpd 2>/dev/null | awk '{print $2}' | tr -d '"' | sed "s|^\${APACHE_LOG_DIR}|/var/log/apache2|"; echo /var/log/nginx/access.log; echo /var/log/apache2/access.log; echo /var/log/apache2/other_vhosts_access.log; echo /var/log/httpd/access_log; find "$HOME" -maxdepth 3 \( -name '*access.log' -o -name '*access_log' \) -type f 2>/dev/null ) | grep -v -e '\$' -e '^off$' -e '^|' | sort -u | while IFS= read -r f; do
  if [ -r "$f" ]; then R="tail -n __LINES__"; elif sudo -n test -r "$f" 2>/dev/null; then R="sudo -n tail -n __LINES__"; else continue; fi
`

// AttacksScript: скрипт журналов за окно since. Пути ботов - те же, что ищет tim audit.
func AttacksScript(since time.Duration) string {
	s := strings.Replace(attacksScript, "__PROBES__", audit.ProbePattern(), 1)
	return fmt.Sprintf("SINCE=%d\n", int(since.Seconds())) + strings.ReplaceAll(s, "__LINES__", "200000")
}
