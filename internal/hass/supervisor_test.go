// Тесты супервизора переподключения.
// Логика модуля: фейковый RunFunc имитирует обрывы, успешные подключения
// и отклонённый токен; проверяются критерии приёмки task-05 —
// N обрывов дают N переподключений, ErrAuthInvalid не ретраится,
// уведомления о простое/восстановлении вызываются по одному разу,
// отмена контекста мгновенно завершает цикл.
package hass

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fastBackoff — бэкофф с микрозадержками для тестов.
func fastBackoff() *Backoff {
	b := NewBackoff()
	b.base = time.Millisecond
	b.max = 2 * time.Millisecond
	b.randFn = func() float64 { return 0.5 }
	return b
}

// TestReconnects: после N обрывов супервизор делает N+1 запусков
// и продолжает работать до отмены контекста.
func TestReconnects(t *testing.T) {
	const failures = 5
	var runs atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Supervisor{
		Backoff:   fastBackoff(),
		DownAfter: time.Hour, // порог простоя в этом тесте не участвует
		Run: func(ctx context.Context, onReady func()) error {
			n := runs.Add(1)
			if n <= failures {
				return errors.New("обрыв соединения")
			}
			// Успешное подключение: сообщаем о готовности и держим
			// соединение до отмены контекста.
			onReady()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() { done <- s.Loop(ctx) }()

	// Дожидаемся успешного (N+1)-го запуска.
	deadline := time.After(5 * time.Second)
	for runs.Load() < failures+1 {
		select {
		case <-deadline:
			t.Fatalf("за отведённое время выполнено только %d запусков", runs.Load())
		case <-time.After(time.Millisecond):
		}
	}

	cancel()
	if err := <-done; err != nil {
		t.Errorf("после отмены контекста Loop должен вернуть nil, получено: %v", err)
	}
	if got := runs.Load(); got != failures+1 {
		t.Errorf("ожидалось ровно %d запусков, выполнено %d", failures+1, got)
	}
}

// TestAuthInvalidNoRetry: отклонённый токен завершает цикл без ретраев.
func TestAuthInvalidNoRetry(t *testing.T) {
	var runs atomic.Int32
	s := &Supervisor{
		Backoff:   fastBackoff(),
		DownAfter: time.Hour,
		Run: func(ctx context.Context, onReady func()) error {
			runs.Add(1)
			return ErrAuthInvalid
		},
	}

	err := s.Loop(context.Background())
	if !errors.Is(err, ErrAuthInvalid) {
		t.Errorf("ожидалась ErrAuthInvalid, получено: %v", err)
	}
	if got := runs.Load(); got != 1 {
		t.Errorf("auth_invalid не должен ретраиться, выполнено %d запусков", got)
	}
}

// TestDownUpNotifications: простой дольше порога даёт ровно одно OnDown,
// восстановление — ровно одно OnUp.
func TestDownUpNotifications(t *testing.T) {
	var downCalls, upCalls atomic.Int32
	var runs atomic.Int32
	const failures = 10 // многократные обрывы в течение одного простоя

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Supervisor{
		Backoff:   fastBackoff(),
		DownAfter: 20 * time.Millisecond,
		OnDown:    func() { downCalls.Add(1) },
		OnUp:      func() { upCalls.Add(1) },
		Run: func(ctx context.Context, onReady func()) error {
			if runs.Add(1) <= failures {
				time.Sleep(5 * time.Millisecond) // растягиваем простой за порог
				return errors.New("обрыв")
			}
			onReady()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() { done <- s.Loop(ctx) }()

	// Дожидаемся восстановления (OnUp) и проверяем счётчики.
	deadline := time.After(5 * time.Second)
	for upCalls.Load() == 0 {
		select {
		case <-deadline:
			t.Fatalf("OnUp не вызван; OnDown вызван %d раз", downCalls.Load())
		case <-time.After(time.Millisecond):
		}
	}
	if got := downCalls.Load(); got != 1 {
		t.Errorf("OnDown должен вызваться ровно один раз за простой, вызван %d", got)
	}
	if got := upCalls.Load(); got != 1 {
		t.Errorf("OnUp должен вызваться ровно один раз, вызван %d", got)
	}

	cancel()
	<-done
}

// TestShortOutageNoNotification: обрыв короче порога уведомлений не порождает.
func TestShortOutageNoNotification(t *testing.T) {
	var downCalls atomic.Int32
	var runs atomic.Int32

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s := &Supervisor{
		Backoff:   fastBackoff(),
		DownAfter: time.Hour, // порог заведомо больше длительности обрыва
		OnDown:    func() { downCalls.Add(1) },
		Run: func(ctx context.Context, onReady func()) error {
			if runs.Add(1) == 1 {
				return errors.New("короткий обрыв")
			}
			onReady()
			<-ctx.Done()
			return ctx.Err()
		},
	}

	done := make(chan error, 1)
	go func() { done <- s.Loop(ctx) }()

	deadline := time.After(5 * time.Second)
	for runs.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("переподключение после короткого обрыва не произошло")
		case <-time.After(time.Millisecond):
		}
	}
	cancel()
	<-done

	if got := downCalls.Load(); got != 0 {
		t.Errorf("короткий обрыв не должен уведомлять, OnDown вызван %d раз", got)
	}
}

// TestCancelDuringBackoff: отмена контекста во время ожидания бэкоффа
// завершает Loop без задержки.
func TestCancelDuringBackoff(t *testing.T) {
	b := NewBackoff() // боевые задержки: первая пауза ~1 секунда
	ctx, cancel := context.WithCancel(context.Background())

	s := &Supervisor{
		Backoff:   b,
		DownAfter: time.Hour,
		Run: func(ctx context.Context, onReady func()) error {
			return errors.New("обрыв") // сразу в ожидание бэкоффа
		},
	}

	done := make(chan error, 1)
	go func() { done <- s.Loop(ctx) }()
	time.Sleep(10 * time.Millisecond) // цикл вошёл в ожидание
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("после отмены ожидался nil, получено: %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Loop не завершился сразу после отмены контекста")
	}
}
