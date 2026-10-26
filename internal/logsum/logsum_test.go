package logsum

import "testing"

func TestSummarize(t *testing.T) {
	lines := []string{
		"2026-10-28T03:12:01+0300 timcode sshd[1234]: error: kex_exchange_identification: Connection closed by 185.220.101.4 port 51234",
		"2026-10-28T03:12:09+0300 timcode sshd[1240]: error: kex_exchange_identification: Connection closed by 45.141.84.10 port 40022",
		"2026-10-28T09:30:00+0300 timcode sshd[2001]: error: kex_exchange_identification: Connection closed by 103.77.192.5 port 1022",
		"2026/10/28 04:00:00 [error] 811#811: *5521 open() \"/var/www/shop/.env\" failed (2: No such file or directory), client: 5.188.10.2",
		"[2026-10-28 05:01:02] production.ERROR: SQLSTATE[HY000] [2002] Connection refused (SQL: select * from users where id = 17)",
		"#0 /var/www/shop/vendor/laravel/framework/src/Illuminate/Database/Connection.php(712): Illuminate\\Database\\Connection->runQueryCallback()",
		"[2026-10-28 05:07:40] production.ERROR: SQLSTATE[HY000] [2002] Connection refused (SQL: select * from users where id = 92)",
		"Oct 28 06:00:01 timcode kernel: Out of memory: Killed process 4242 (php-fpm8.3) total-vm:812345kB",
		"2026-10-28T07:00:00+0300 timcode php8.3-fpm[700]: WARNING: [pool www] child 3e1f9a2b exited on signal 9",
	}
	pats := Summarize(lines)
	if len(pats) != 5 {
		for _, p := range pats {
			t.Log(p.Count, p.Text)
		}
		t.Fatalf("шаблонов %d, ожидали 5", len(pats))
	}
	ssh := pats[0]
	if ssh.Count != 3 || ssh.Text != "sshd[N]: error: kex_exchange_identification: Connection closed by <ip> port N" {
		t.Errorf("ssh: %d %q", ssh.Count, ssh.Text)
	}
	if ssh.First.Format("15:04") != "03:12" || ssh.Last.Format("15:04") != "09:30" {
		t.Errorf("ssh время: %v - %v", ssh.First, ssh.Last)
	}
	if sql := pats[1]; sql.Count != 2 || sql.Text != "production.ERROR: SQLSTATE[HY000] [N] Connection refused (SQL: select * from users where id = N)" {
		t.Errorf("laravel: %d %q", sql.Count, sql.Text)
	}
	if fpm := pats[4]; fpm.Text != "php8.3-fpm[N]: WARNING: [pool www] child <hex> exited on signal N" || fpm.Sample[:10] != "php8.3-fpm" {
		t.Errorf("fpm: %q / %q", fpm.Text, fpm.Sample)
	}
	if oom := pats[3]; oom.Text != "kernel: Out of memory: Killed process N (php-fpm8.3) total-vm:812345kB" {
		t.Errorf("oom: %q", oom.Text)
	}
}
