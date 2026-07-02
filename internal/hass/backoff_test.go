// Тесты бэкоффа переподключения.
// Логика модуля: детерминированная проверка последовательности задержек
// (рост до потолка 60с), границ джиттера ±20% и сброса — критерии
// приёмки task-05.
package hass

import (
	"testing"
	"time"
)

// newTestBackoff создаёт бэкофф с подменённым источником случайности.
func newTestBackoff(randVal float64) *Backoff {
	b := NewBackoff()
	b.randFn = func() float64 { return randVal }
	return b
}

// TestBackoffSequence: при нейтральном джиттере (rand=0.5 → множитель 1.0)
// последовательность ровно 1, 2, 4, …, 60, 60 секунд.
func TestBackoffSequence(t *testing.T) {
	b := newTestBackoff(0.5)
	want := []time.Duration{
		1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second,
		16 * time.Second, 32 * time.Second, 60 * time.Second, 60 * time.Second,
	}
	for i, w := range want {
		if got := b.Next(); got != w {
			t.Errorf("шаг %d: Next() = %v, ожидалось %v", i, got, w)
		}
	}
}

// TestBackoffJitterBounds: джиттер не выводит задержку за пределы ±20%.
func TestBackoffJitterBounds(t *testing.T) {
	// rand=0 → нижняя граница (0.8x), rand≈1 → верхняя (до 1.2x).
	low := newTestBackoff(0).Next()
	if low != time.Duration(float64(time.Second)*0.8) {
		t.Errorf("нижняя граница джиттера: %v, ожидалось 800ms", low)
	}
	high := newTestBackoff(0.999999).Next()
	if high < time.Second || high > time.Duration(float64(time.Second)*1.2) {
		t.Errorf("верхняя граница джиттера: %v, ожидалось (1s, 1.2s]", high)
	}
}

// TestBackoffReset: после Reset последовательность начинается с базы.
func TestBackoffReset(t *testing.T) {
	b := newTestBackoff(0.5)
	for i := 0; i < 5; i++ {
		b.Next()
	}
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Errorf("после Reset ожидалась задержка 1s, получено %v", got)
	}
}
