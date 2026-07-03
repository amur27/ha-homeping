// Юнит-тесты супервизора верхнего уровня.
// Логика модуля: проверка смены статусов, hot-reload (включая отклонение
// невалидного конфига без прерывания работы), поведения при auth_invalid
// в обоих режимах (трей/FailFast) и паузы уведомлений. Сессия и получение
// токена подменяются фейками — сеть и системное хранилище не используются.
package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"homeping/internal/config"
	"homeping/internal/hass"
)

// waitTimeout — общий таймаут ожиданий в тестах.
const waitTimeout = 3 * time.Second

// fakeNotifier запоминает показанные уведомления.
type fakeNotifier struct {
	mu    sync.Mutex
	shown []string
}

func (f *fakeNotifier) Show(title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shown = append(f.shown, title+": "+body)
	return nil
}

func (f *fakeNotifier) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.shown)
}

// validYAML — минимальный валидный конфиг; интервал троттлинга различает версии.
func validYAML(interval int) string {
	return fmt.Sprintf(`homeassistant:
  url: "ws://ha.local:8123/api/websocket"
entities:
  - id: binary_sensor.door
    states:
      "on": "Открыта"
notifications:
  min_interval_sec: %d
`, interval)
}

// writeConfig записывает содержимое конфига в путь теста.
func writeConfig(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitStatus ждёт, пока статус агента не удовлетворит предикату.
func waitStatus(t *testing.T, a *Agent, want func(Status) bool, describe string) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if want(a.Status()) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("не дождались состояния «%s»; текущее: %+v", describe, a.Status())
}

// sessionRecord — параметры одного запуска фейковой сессии.
type sessionRecord struct {
	cfg   *config.Config
	token string
}

// fakeSessions собирает запуски сессий; каждая блокируется до отмены контекста.
type fakeSessions struct {
	mu   sync.Mutex
	runs []sessionRecord
}

func (f *fakeSessions) run(ctx context.Context, cfg *config.Config, token string) error {
	f.mu.Lock()
	f.runs = append(f.runs, sessionRecord{cfg: cfg, token: token})
	f.mu.Unlock()
	<-ctx.Done()
	return nil
}

func (f *fakeSessions) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func (f *fakeSessions) last() sessionRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.runs[len(f.runs)-1]
}

// newTestAgent собирает агента с фейковыми токеном и сессией.
func newTestAgent(path string, sessions *fakeSessions) (*Agent, *fakeNotifier) {
	n := &fakeNotifier{}
	a := &Agent{
		ConfigPath:   path,
		Notifier:     n,
		resolveToken: func(*config.Config) (string, error) { return "токен", nil },
		runSession:   sessions.run,
	}
	return a, n
}

// TestReloadRestartsSession: Reload перечитывает конфиг и перезапускает сессию.
func TestReloadRestartsSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, validYAML(3))
	sessions := &fakeSessions{}
	a, _ := newTestAgent(path, sessions)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	waitStatus(t, a, func(s Status) bool { return s.State == StateConnecting }, "подключение")
	if sessions.count() != 1 {
		t.Fatalf("ожидалась одна сессия, запущено %d", sessions.count())
	}
	if sessions.last().cfg.Notifications.MinIntervalSec != 3 {
		t.Fatalf("сессия получила неожиданный конфиг: %+v", sessions.last().cfg.Notifications)
	}

	// Меняем конфиг и перезагружаем — должна подняться вторая сессия с новым интервалом.
	writeConfig(t, path, validYAML(7))
	if err := a.Reload(); err != nil {
		t.Fatalf("Reload валидного конфига: %v", err)
	}
	deadline := time.Now().Add(waitTimeout)
	for sessions.count() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if sessions.count() != 2 {
		t.Fatalf("после Reload ожидалась вторая сессия, запущено %d", sessions.count())
	}
	if sessions.last().cfg.Notifications.MinIntervalSec != 7 {
		t.Fatalf("вторая сессия получила старый конфиг: %+v", sessions.last().cfg.Notifications)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run после отмены контекста: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Run не завершился после отмены контекста")
	}
}

// TestReloadInvalidKeepsRunning: невалидный конфиг при Reload — ошибка,
// текущая сессия продолжает работать (docs/spec.md, раздел 10).
func TestReloadInvalidKeepsRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, validYAML(3))
	sessions := &fakeSessions{}
	a, _ := newTestAgent(path, sessions)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)
	waitStatus(t, a, func(s Status) bool { return s.State == StateConnecting }, "подключение")

	writeConfig(t, path, "мусор: [незакрытый")
	if err := a.Reload(); err == nil {
		t.Fatal("Reload невалидного конфига должен вернуть ошибку")
	}
	// Сессия не перезапустилась и не умерла.
	time.Sleep(50 * time.Millisecond)
	if sessions.count() != 1 {
		t.Fatalf("сессий: %d, невалидный reload не должен перезапускать сессию", sessions.count())
	}
	if a.Status().State != StateConnecting {
		t.Fatalf("статус изменился на %v, агент должен работать как прежде", a.Status().State)
	}
}

// TestAuthErrorWaitsForReload: в трей-режиме auth_invalid не завершает агента —
// статус «ошибка токена», уведомление, после Reload сессия запускается снова.
func TestAuthErrorWaitsForReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, validYAML(3))

	n := &fakeNotifier{}
	var failFirst sync.Once
	sessions := &fakeSessions{}
	a := &Agent{
		ConfigPath:   path,
		Notifier:     n,
		resolveToken: func(*config.Config) (string, error) { return "токен", nil },
	}
	// Первая сессия падает с auth_invalid, последующие — обычные фейковые.
	a.runSession = func(ctx context.Context, cfg *config.Config, token string) error {
		fail := false
		failFirst.Do(func() { fail = true })
		if fail {
			return hass.ErrAuthInvalid
		}
		return sessions.run(ctx, cfg, token)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go a.Run(ctx)

	waitStatus(t, a, func(s Status) bool { return s.State == StateAuthError }, "ошибка токена")
	if n.count() != 1 {
		t.Fatalf("ожидалось одно уведомление об ошибке токена, показано %d", n.count())
	}

	if err := a.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	waitStatus(t, a, func(s Status) bool { return s.State == StateConnecting }, "повторное подключение")
	if sessions.count() != 1 {
		t.Fatalf("после Reload ожидалась новая сессия, запущено %d", sessions.count())
	}
}

// TestFailFast: в headless-режиме ошибки конфига и токена фатальны.
func TestFailFast(t *testing.T) {
	// Отсутствующий конфиг → ErrConfig (код выхода 2).
	a := &Agent{
		ConfigPath: filepath.Join(t.TempDir(), "нет.yaml"),
		Notifier:   &fakeNotifier{},
		FailFast:   true,
	}
	if err := a.Run(context.Background()); !errors.Is(err, ErrConfig) {
		t.Fatalf("Run без конфига: ожидалась ErrConfig, получено %v", err)
	}

	// auth_invalid → ошибка наружу (код выхода 3).
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig(t, path, validYAML(3))
	a = &Agent{
		ConfigPath:   path,
		Notifier:     &fakeNotifier{},
		FailFast:     true,
		resolveToken: func(*config.Config) (string, error) { return "токен", nil },
		runSession: func(context.Context, *config.Config, string) error {
			return hass.ErrAuthInvalid
		},
	}
	if err := a.Run(context.Background()); !errors.Is(err, hass.ErrAuthInvalid) {
		t.Fatalf("Run при auth_invalid: ожидалась hass.ErrAuthInvalid, получено %v", err)
	}
}

// TestPauseGate: пауза подавляет уведомления шлюза, но не пробное уведомление.
func TestPauseGate(t *testing.T) {
	n := &fakeNotifier{}
	a := &Agent{Notifier: n}
	gate := pauseGate{a}

	a.Pause(true)
	if !a.Status().Paused {
		t.Fatal("статус не отражает включённую паузу")
	}
	if err := gate.Show("Дверь", "Открыта"); err != nil {
		t.Fatalf("Show при паузе: %v", err)
	}
	if n.count() != 0 {
		t.Fatalf("пауза включена, но уведомление показано")
	}
	// Пробное уведомление — явный запрос пользователя, показывается всегда.
	if err := a.TestNotification(); err != nil || n.count() != 1 {
		t.Fatalf("пробное уведомление при паузе: показано %d, err=%v", n.count(), err)
	}

	a.Pause(false)
	if err := gate.Show("Дверь", "Открыта"); err != nil || n.count() != 2 {
		t.Fatalf("после снятия паузы уведомление не показано: %d, err=%v", n.count(), err)
	}
}

// TestStatusListener: подписчик получает изменения состояния.
func TestStatusListener(t *testing.T) {
	a := &Agent{Notifier: &fakeNotifier{}}
	var mu sync.Mutex
	var got []State
	a.OnStatusChange(func(s Status) {
		mu.Lock()
		got = append(got, s.State)
		mu.Unlock()
	})

	a.setStatus(StateConnecting, "")
	a.setStatus(StateConnected, "")

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 || got[0] != StateConnecting || got[1] != StateConnected {
		t.Fatalf("подписчик получил %v, ожидались подключение → подключён", got)
	}
}
