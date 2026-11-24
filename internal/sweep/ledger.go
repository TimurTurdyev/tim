package sweep

type nameState uint8

const (
	flying  nameState = iota // запрос в пути
	queued                   // ждёт повтора
	viaTCP                   // ответ был обрезан: ждёт своей очереди по TCP
	settled                  // есть ответ по существу
	dropped                  // попытки кончились или имя не годится для запроса
)

// ledger: учёт имён - что выдано, на что есть ответ, что брошено. Имена выдаются строго по порядку.
type ledger struct {
	names   []string
	answers []Answer
	state   []nameState
	tries   []uint8
	// open: выданы и ещё ждут ответа
	open int
}

func newLedger(names []string) ledger {
	return ledger{names: names, state: make([]nameState, len(names)), tries: make([]uint8, len(names))}
}

// issue: следующее новое имя; негодное для запроса сразу получает отказ.
func (l *ledger) issue() (int32, bool) {
	for l.left() {
		idx := int32(len(l.answers))
		name := l.names[idx]
		if packable(name) {
			l.answers = append(l.answers, Answer{Name: name})
			l.open++
			return idx, true
		}
		l.answers = append(l.answers, Answer{Name: name, Kind: Fail})
		l.state[idx] = dropped
	}
	return 0, false
}

func (l *ledger) left() bool { return len(l.answers) < len(l.names) }

// settle: ответ по существу; false - на это имя он уже был.
func (l *ledger) settle(idx int32, a Answer) bool {
	switch l.state[idx] {
	case settled:
		return false
	case dropped:
	default:
		l.open--
	}
	a.Name = l.names[idx]
	l.answers[idx], l.state[idx] = a, settled
	return true
}

func (l *ledger) drop(idx int32) {
	l.state[idx] = dropped
	l.open--
}

func (l *ledger) noteFail(idx int32) {
	if l.state[idx] != settled {
		l.answers[idx].Kind = Fail
	}
}

// progress: сколько имён получили окончательный итог и сколько их всего.
func (l *ledger) progress() (done, total int) { return len(l.answers) - l.open, len(l.names) }

func (l *ledger) result() Result {
	res := Result{Answers: l.answers}
	for i := range l.answers {
		if !definite(l.answers[i].Kind) {
			res.Unanswered++
		}
	}
	return res
}
