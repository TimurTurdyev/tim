package servers

import (
	"net"
	"slices"

	"github.com/TimurTurdyev/tim/internal/runner"
	"github.com/TimurTurdyev/tim/internal/shellq"
	"github.com/TimurTurdyev/tim/internal/srvinfo"
	"github.com/TimurTurdyev/tim/internal/ui"
)

// Бан - единственная запись на сервер из tim: стандартными ufw, fail2ban или CrowdSec, со сроком, с окном
// подтверждения, где видна команда; --dry-run только печатает.

// banPlan: чем и как банить адрес.
type banPlan struct {
	Args []string // команда на сервере под sudo
	Term string   // на сколько
	Undo string   // как снять
}

// planBan: CrowdSec, иначе fail2ban, иначе ufw - по тому, что работает на сервере.
func planBan(target, ip string, a *srvinfo.Attacks) (banPlan, error) {
	if net.ParseIP(ip) == nil {
		return banPlan{}, ui.Errorf("%s - не адрес", ip)
	}
	r := srvinfo.Report{}
	if s, _, ok := saved(target); ok {
		r = s.Report
	}
	crowdsec, f2b := r.CrowdSec, r.Fail2ban
	jail := "sshd"
	if a != nil {
		crowdsec, f2b = crowdsec || a.CrowdSec, f2b || a.Fail2ban
		if len(a.Jails) > 0 && !slices.ContainsFunc(a.Jails, func(j srvinfo.Jail) bool { return j.Name == "sshd" }) {
			jail = a.Jails[0].Name
		}
	}
	switch {
	case crowdsec:
		return banPlan{Args: []string{"cscli", "decisions", "add", "--ip", ip, "--duration", "168h", "--reason", "tim"},
			Term: "на 7 дн", Undo: "сам снимется через 7 дн; раньше - cscli decisions delete --ip " + ip}, nil
	case f2b:
		return banPlan{Args: []string{"fail2ban-client", "set", jail, "banip", ip},
			Term: "на срок клетки " + jail, Undo: "сам снимется по bantime клетки; раньше - fail2ban-client set " + jail + " unbanip " + ip}, nil
	case r.UFW == "active":
		return banPlan{Args: []string{"ufw", "insert", "1", "deny", "from", ip},
			Term: "бессрочно", Undo: "у ufw срока нет, снять: ufw delete deny from " + ip}, nil
	}
	return banPlan{}, ui.Errorf("%s: нечем банить - нет ни CrowdSec, ни fail2ban, ни включённого ufw", target)
}

// Command: что выполнится - целиком, как в окне подтверждения.
func (p banPlan) Command(target string) string {
	return "ssh -t " + target + " sudo " + shellq.Join(p.Args)
}

// ban выполняет бан в терминале: sudo на сервере может спросить пароль.
func ban(target string, p banPlan) error {
	return runner.Exec("ssh", append([]string{"-t", target, "sudo"}, p.Args...)...)
}

// banCommand: tim server <алиас> --ban <адрес>.
func banCommand(target, ip string) error {
	p, err := planBan(target, ip, nil)
	if err != nil {
		return err
	}
	ui.Println(ui.Dim("выполнится: "+p.Command(target)) + "\n" + ui.Dim(p.Undo))
	if !runner.DryRun && !runner.Confirm("забанить "+ip+" на "+target+" "+p.Term+"?") {
		return ui.Errorf("не забанил: без согласия; в скрипте - с -y")
	}
	return ban(target, p)
}

// banNote: что сказать про sudo в окне.
func banNote(p banPlan) string {
	return p.Undo + "; tim закроется, пароль sudo, если нужен, спросит ssh"
}

