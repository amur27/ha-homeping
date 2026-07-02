// Супервизор соединения с Home Assistant.
// Логика модуля: бесконечный цикл «подключение → обрыв → бэкофф → повтор»
// поверх одного запуска клиента; отслеживание длительности простоя
// и одноразовые уведомления «HA недоступен» / «связь восстановлена»
// (docs/spec.md, разделы 5 и 6). ErrAuthInvalid не ретраится — токен плохой.
package hass

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// RunFunc — одно подключение клиента; onReady вызывается после успешной
// аутентификации и оформления подписок.
type RunFunc func(ctx context.Context, onReady func()) error

// Supervisor управляет жизненным циклом соединения.
type Supervisor struct {
	// Run выполняет одно подключение (обёртка над Client.Run);
	// вынесен в поле-функцию, чтобы тесты подставляли фейковый клиент.
	Run RunFunc
	// Backoff — генератор задержек переподключения.
	Backoff *Backoff
	// DownAfter — порог простоя, после которого вызывается OnDown
	// (по спецификации — 30 секунд).
	DownAfter time.Duration
	// OnDown вызывается один раз, когда связь отсутствует дольше DownAfter.
	// OnUp вызывается при восстановлении, только если OnDown уже сработал.
	// Оба могут быть nil (уведомления об обрыве выключены в конфиге).
	OnDown func()
	OnUp   func()

	mu           sync.Mutex
	downTimer    *time.Timer // таймер порога простоя; идёт — значит, связь потеряна
	downNotified bool        // OnDown уже вызван для текущего простоя
}

// Loop крутит цикл переподключения до отмены контекста (возврат nil)
// или до фатальной ошибки аутентификации (возврат ErrAuthInvalid).
func (s *Supervisor) Loop(ctx context.Context) error {
	for {
		err := s.Run(ctx, s.handleReady)

		if errors.Is(err, ErrAuthInvalid) {
			return err // ретраить бессмысленно — процесс завершается кодом 3
		}
		if ctx.Err() != nil {
			s.stopDownTimer()
			return nil // штатное завершение по сигналу
		}

		s.handleDisconnect()
		delay := s.Backoff.Next()
		slog.Warn("соединение потеряно, переподключение",
			"error", err, "через", delay.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			s.stopDownTimer()
			return nil
		case <-time.After(delay):
		}
	}
}

// handleReady вызывается клиентом после успешных аутентификации и подписок:
// сбрасывает бэкофф и, если пользователь был уведомлён о простое,
// сообщает о восстановлении.
func (s *Supervisor) handleReady() {
	s.Backoff.Reset()

	s.mu.Lock()
	if s.downTimer != nil {
		s.downTimer.Stop()
		s.downTimer = nil
	}
	wasNotified := s.downNotified
	s.downNotified = false
	s.mu.Unlock()

	if wasNotified && s.OnUp != nil {
		s.OnUp()
	}
}

// handleDisconnect фиксирует начало простоя: взводит таймер порога DownAfter,
// по срабатыванию которого пользователь уведомляется один раз.
func (s *Supervisor) handleDisconnect() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downTimer != nil || s.downNotified {
		return // простой уже отслеживается или уведомление уже показано
	}
	s.downTimer = time.AfterFunc(s.DownAfter, func() {
		s.mu.Lock()
		s.downTimer = nil
		s.downNotified = true
		s.mu.Unlock()
		if s.OnDown != nil {
			s.OnDown()
		}
	})
}

// stopDownTimer останавливает таймер простоя при завершении цикла,
// чтобы OnDown не сработал после выхода из Loop.
func (s *Supervisor) stopDownTimer() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.downTimer != nil {
		s.downTimer.Stop()
		s.downTimer = nil
	}
}
