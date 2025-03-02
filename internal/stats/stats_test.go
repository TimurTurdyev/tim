package stats

import (
	"math"
	"testing"
	"time"
)

func TestForecast(t *testing.T) {
	t0 := time.Date(2025, 10, 2, 0, 0, 0, 0, time.UTC)
	var list []Sample
	// Свободное место уходит на 2 ГБ в сутки, плюс один скачок вверх (почистили кеш) посередине.
	for h := 0; h <= 7*24; h += 6 {
		free := int64(100<<30) - int64(h)*(2<<30)/24
		if h == 84 {
			free += 30 << 30
		}
		list = append(list, Sample{At: t0.Add(time.Duration(h) * time.Hour), DiskFree: free})
	}
	perDay, left, ok := Forecast(list)
	if !ok {
		t.Fatal("данных достаточно")
	}
	gb := perDay / (1 << 30)
	if gb > -1.5 || gb < -2.5 {
		t.Fatalf("скорость %.2f ГБ/сут, ожидали около -2", gb)
	}
	if left < 30 || left > 50 {
		t.Fatalf("осталось %.1f сут", left)
	}
}

func TestForecastNotEnough(t *testing.T) {
	t0 := time.Now()
	short := []Sample{{At: t0, DiskFree: 10}, {At: t0.Add(time.Hour), DiskFree: 9}}
	if _, _, ok := Forecast(short); ok {
		t.Fatal("два замера за час - не повод для прогноза")
	}
	var growing []Sample
	for d := 0; d < 7; d++ {
		growing = append(growing, Sample{At: t0.AddDate(0, 0, d), DiskFree: int64(10+d) << 30})
	}
	if _, left, ok := Forecast(growing); !ok || !math.IsInf(left, 1) {
		t.Fatalf("место растёт - конца нет: %v %v", left, ok)
	}
}
