// Экспоненциальный бэкофф переподключения.
// Логика модуля: последовательность задержек 1с → 2с → 4с → … до потолка 60с
// с джиттером ±20% (docs/spec.md, раздел 6); после успешного подключения
// супервизор сбрасывает последовательность в начало.
package hass

import (
	"math/rand/v2"
	"time"
)

// Параметры бэкоффа по умолчанию (docs/spec.md, раздел 6).
const (
	backoffBase   = 1 * time.Second
	backoffMax    = 60 * time.Second
	backoffJitter = 0.2 // ±20%
)

// Backoff — генератор задержек переподключения. Не потокобезопасен:
// используется только из цикла супервизора.
type Backoff struct {
	base   time.Duration
	max    time.Duration
	jitter float64
	// randFn подменяется в тестах для детерминированного джиттера;
	// возвращает значение в [0, 1).
	randFn func() float64

	next time.Duration // следующая базовая задержка (без джиттера)
}

// NewBackoff создаёт бэкофф с параметрами из спецификации.
func NewBackoff() *Backoff {
	return &Backoff{
		base:   backoffBase,
		max:    backoffMax,
		jitter: backoffJitter,
		randFn: rand.Float64,
	}
}

// Next возвращает очередную задержку: базовое значение удваивается
// до потолка, к результату применяется джиттер ±jitter.
func (b *Backoff) Next() time.Duration {
	if b.next == 0 {
		b.next = b.base
	}
	d := b.next
	b.next *= 2
	if b.next > b.max {
		b.next = b.max
	}
	// Джиттер: множитель в диапазоне [1-jitter, 1+jitter).
	factor := 1 + b.jitter*(2*b.randFn()-1)
	return time.Duration(float64(d) * factor)
}

// Reset возвращает последовательность к началу (вызывается после
// успешной аутентификации и подписок).
func (b *Backoff) Reset() {
	b.next = 0
}
