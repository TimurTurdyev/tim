package srvinfo

import (
	"fmt"
	"time"
)

// attacksScript читает журналы за окно SINCE (секунды) и сворачивает счёт на сервере: за сутки перебора
// бывают десятки тысяч строк, по ssh едут только итоги. Удачные входы и пробы сайта - строками, их мало.
// Только чтение; где нужен root - sudo -n, который без пароля отказывает, а не спрашивает.
const attacksScript = `export LC_ALL=C
J() { journalctl -q --no-pager -o short-iso --since "-${SINCE}s" "$@" 2>/dev/null; }
SJ() { sudo -n journalctl -q --no-pager -o short-iso --since "-${SINCE}s" "$@" 2>/dev/null; }
if [ -n "$(J -u ssh -u sshd -n 1)" ]; then SSH="J -u ssh -u sshd"; SRC=journal
elif [ -n "$(SJ -u ssh -u sshd -n 1)" ]; then SSH="SJ -u ssh -u sshd"; SRC=journal-sudo
elif [ -r /var/log/auth.log ]; then SSH="cat /var/log/auth.log"; SRC=authlog
elif sudo -n test -r /var/log/auth.log 2>/dev/null; then SSH="sudo -n cat /var/log/auth.log"; SRC=authlog-sudo
else SRC=none; fi
FAIL='Failed password|Invalid user|maximum authentication attempts'
echo @@sshsrc; echo "$SRC"
if [ "$SRC" != none ]; then
  echo @@sshfail; eval "$SSH" | grep -E "$FAIL" | grep -oE 'from [0-9a-fA-F.:]+' | awk '{print $2}' | sort | uniq -c | sort -rn | awk 'NR<=50{print} END{print "total", NR}'
  echo @@sshfailtotal; eval "$SSH" | grep -cE "$FAIL"
  echo @@sshusers; eval "$SSH" | sed -nE 's/.*(Invalid user|Failed password for (invalid user )?)([^ ]+) from .*/\3/p' | sort | uniq -c | sort -rn | head -10
  echo @@sshok; eval "$SSH" | grep -E 'Accepted (password|publickey|keyboard-interactive)' | tail -n 200
fi
echo @@probes
P='\.env|/\.git/|wp-login\.php|xmlrpc\.php|phpmyadmin|/pma/|adminer|/\.aws/|/server-status|phpunit|eval-stdin|/\.DS_Store|backup\.(sql|zip)|dump\.sql|/_ignition|/telescope|/actuator|/cgi-bin/'
for f in $( (grep -rhoE '^[[:space:]]*access_log[[:space:]]+[^ ;]+' /etc/nginx 2>/dev/null | awk '{print $2}'; echo /var/log/nginx/access.log; find "$HOME" -maxdepth 3 -name '*access.log' -type f 2>/dev/null) | grep -v -e '\$' -e '^off$' | sort -u); do
  if [ -r "$f" ]; then R="tail -n 200000 $f"; elif sudo -n test -r "$f" 2>/dev/null; then R="sudo -n tail -n 200000 $f"; else continue; fi
  echo "file $f"
  eval "$R" | grep -E "\"[A-Z]+ [^\"]*($P)" | tail -n 3000 | sed "s|^|$f |"
done
echo @@ufw
if [ -n "$(J -k -n 1)" ]; then U="J -k"; elif [ -n "$(SJ -k -n 1)" ]; then U="SJ -k"; elif [ -r /var/log/ufw.log ]; then U="cat /var/log/ufw.log"; else U=""; fi
if [ -n "$U" ]; then
  echo "total $(eval "$U" | grep -c 'UFW BLOCK')"
  eval "$U" | grep 'UFW BLOCK' | grep -oE 'SRC=[^ ]+' | cut -d= -f2 | sort | uniq -c | sort -rn | head -30 | sed 's/^/src /'
  eval "$U" | grep 'UFW BLOCK' | grep -oE 'DPT=[0-9]+' | cut -d= -f2 | sort | uniq -c | sort -rn | head -15 | sed 's/^/dpt /'
else echo none; fi
echo @@f2b
if ! command -v fail2ban-client >/dev/null 2>&1; then echo none
elif ! sudo -n fail2ban-client status >/dev/null 2>&1; then echo nosudo
else for j in $(sudo -n fail2ban-client status | sed -n 's/.*Jail list:[[:space:]]*//p' | tr ',' ' '); do echo "jail $j"; sudo -n fail2ban-client status "$j" | grep -E 'Currently banned|Total banned|Banned IP list'; done; fi
echo @@cscli
if ! command -v cscli >/dev/null 2>&1; then echo none
elif ! out=$(sudo -n cscli decisions list -o json 2>/dev/null); then echo nosudo
else echo "$out"; fi
echo @@prot
echo "fail2ban $(systemctl is-active fail2ban 2>/dev/null)"; echo "crowdsec $(systemctl is-active crowdsec 2>/dev/null)"
sudo -n sshd -T 2>/dev/null | grep -Ei '^(passwordauthentication|kbdinteractiveauthentication) ' || echo nosudo
echo @@end
`

// AttacksScript: скрипт журналов за окно since.
func AttacksScript(since time.Duration) string {
	return fmt.Sprintf("SINCE=%d\n", int(since.Seconds())) + attacksScript
}
