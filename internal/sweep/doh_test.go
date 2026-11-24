package sweep

import (
	"testing"
	"time"
)

const (
	beat = 50 * time.Millisecond
	ms   = time.Millisecond
)

func TestDeskRepeatsRefusedLater(t *testing.T) {
	d := newDesk([]string{word(0), word(1)}, 0)
	first, _ := d.next(0)
	if !d.take(first, Answer{Kind: Fail}, true, beat, 100*ms) {
		t.Fatal("отказ резолвера не назван поводом для паузы")
	}
	second, _ := d.next(200 * ms)
	if second == idle || second == first {
		t.Fatalf("после отказа выдано %d: до паузы имя не повторяется, идёт следующее", second)
	}
	d.take(second, Answer{Kind: NX}, true, beat, 250*ms)
	if idx, wait := d.next(300 * ms); idx != idle || wait <= 0 || d.finished(300*ms) {
		t.Fatalf("до конца паузы выдано %d, ждать %v", idx, wait)
	}
	now := 100*ms + dohPause
	for range triesResolver - 1 {
		idx, _ := d.next(now)
		if idx != first {
			t.Fatalf("после паузы выдано %d, а не отказанное имя", idx)
		}
		d.take(idx, Answer{Kind: Fail}, true, beat, now)
		now += dohPause
	}
	res := d.book.result()
	if !d.finished(now) || res.Unanswered != 1 || res.Answers[first].Kind != Fail || res.Answers[second].Kind != NX {
		t.Errorf("после %d отказов: закончено %v, итог %+v", triesResolver, d.finished(now), res)
	}
}

func TestDeskLink(t *testing.T) {
	names := make([]string, 100)
	for i := range names {
		names[i] = word(i)
	}
	d := newDesk(names, 0)
	var flying []int32
	for range 8 {
		idx, _ := d.next(0)
		flying = append(flying, idx)
	}
	// соединение оборвалось: все запросы в пути падают разом, и это один сбой, а не восемь
	for _, idx := range flying {
		d.take(idx, Answer{}, false, beat, time.Second)
	}
	if d.broken() {
		t.Fatal("обрыв одного соединения принят за нерабочий адрес")
	}
	if idx, _ := d.next(time.Second + dohPause/2); idx != idle {
		t.Fatalf("сразу после обрыва выдано имя %d", idx)
	}
	now := time.Second + dohPause
	lone, _ := d.next(now)
	if again, _ := d.next(now); lone == idle || again != idle {
		t.Fatalf("после обрыва запросы идут по одному: выданы %d и %d", lone, again)
	}
	d.take(lone, Answer{Kind: NX}, true, beat, now+beat)
	one, _ := d.next(now + beat)
	two, _ := d.next(now + beat)
	if one == idle || two == idle {
		t.Fatalf("резолвер ответил, а раздача не пошла: %d и %d", one, two)
	}
	// теперь резолвер пропал совсем: одиночные запросы без ответа идут в счёт
	d.take(one, Answer{}, false, beat, now+time.Second)
	d.take(two, Answer{}, false, beat, now+time.Second)
	now += time.Second
	for range dohStrikes {
		if d.broken() {
			t.Fatal("адрес назван нерабочим раньше времени")
		}
		now += dohPause
		idx, _ := d.next(now)
		if idx == idle {
			t.Fatal("одиночный запрос не выдан")
		}
		d.take(idx, Answer{}, false, beat, now)
	}
	if !d.broken() {
		t.Errorf("%d одиночных запроса подряд без ответа, а адрес рабочий", dohStrikes)
	}
}

func TestDeskBudget(t *testing.T) {
	names := make([]string, 100)
	for i := range names {
		names[i] = word(i)
	}
	d := newDesk(names, 10*time.Second)
	idx, _ := d.next(0)
	d.take(idx, Answer{Kind: NX}, true, 300*ms, 300*ms)
	if idx, _ := d.next(8 * time.Second); idx == idle {
		t.Fatal("за две секунды до срока новое имя не выдано")
	}
	// ответы идут 300 мс: за 200 мс до срока новый запрос вернуться не успеет
	if idx, _ := d.next(9800 * ms); idx != idle {
		t.Errorf("за 200 мс до срока выдано имя %d", idx)
	}
	if d.finished(9800*ms) || !d.finished(10*time.Second) {
		t.Error("перебор кончается в срок: не раньше, пока запрос в пути, и не позже")
	}
	if res := d.book.result(); len(res.Answers) != 2 || res.Unanswered != 1 {
		t.Errorf("взято %d имён, без ответа %d", len(res.Answers), res.Unanswered)
	}
}
