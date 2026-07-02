// Тесты маршрутизатора уведомлений.
// Логика модуля: фейковый Notifier фиксирует показанные уведомления;
// проверяются маппинг states, шаблон template, отсутствие уведомления
// для состояния без маппинга и схлопывание шквала событий троттлингом
// (короткие интервалы вместо фейковых часов — допускается task-04).
package notify

import (
	"sync"
	"testing"
	"time"

	"ha-notify-agent/internal/config"
)

// fakeNotifier потокобезопасно записывает все показанные уведомления.
type fakeNotifier struct {
	mu    sync.Mutex
	calls []message
}

func (f *fakeNotifier) Show(title, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, message{title: title, body: body})
	return nil
}

func (f *fakeNotifier) snapshot() []message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]message(nil), f.calls...)
}

// testEntities — минимальный набор сущностей для тестов.
func testEntities() []config.Entity {
	return []config.Entity{
		{
			ID:   "binary_sensor.front_door",
			Name: "Входная дверь",
			States: map[string]string{
				"on":  "Дверь открыта",
				"off": "Дверь закрыта",
			},
		},
		{
			ID:       "sensor.temp",
			Name:     "Температура",
			Template: "Сейчас {state} °C",
		},
	}
}

// TestStatesMapping: состояние из маппинга даёт уведомление с нужным текстом.
func TestStatesMapping(t *testing.T) {
	fake := &fakeNotifier{}
	r := NewRouter(testEntities(), fake, 0) // без троттлинга

	r.Handle("binary_sensor.front_door", "on")

	calls := fake.snapshot()
	if len(calls) != 1 {
		t.Fatalf("ожидалось 1 уведомление, показано %d", len(calls))
	}
	if calls[0].title != "Входная дверь" || calls[0].body != "Дверь открыта" {
		t.Errorf("уведомление %+v не соответствует маппингу", calls[0])
	}
}

// TestTemplate: шаблон подставляет состояние вместо {state}.
func TestTemplate(t *testing.T) {
	fake := &fakeNotifier{}
	r := NewRouter(testEntities(), fake, 0)

	r.Handle("sensor.temp", "23.5")

	calls := fake.snapshot()
	if len(calls) != 1 {
		t.Fatalf("ожидалось 1 уведомление, показано %d", len(calls))
	}
	if calls[0].body != "Сейчас 23.5 °C" {
		t.Errorf("шаблон подставлен неверно: %q", calls[0].body)
	}
}

// TestUnmappedState: состояние без записи в states уведомления не порождает.
func TestUnmappedState(t *testing.T) {
	fake := &fakeNotifier{}
	r := NewRouter(testEntities(), fake, 0)

	r.Handle("binary_sensor.front_door", "unavailable")
	r.Handle("unknown.entity", "on") // сущность вне конфига — тоже тишина

	if calls := fake.snapshot(); len(calls) != 0 {
		t.Errorf("уведомлений быть не должно, показано %d: %+v", len(calls), calls)
	}
}

// TestThrottle: шквал событий одной сущности внутри интервала схлопывается —
// показывается первое сразу и последнее по истечении интервала.
func TestThrottle(t *testing.T) {
	fake := &fakeNotifier{}
	interval := 80 * time.Millisecond
	r := NewRouter(testEntities(), fake, interval)

	// Дребезг: on/off/on подряд.
	r.Handle("binary_sensor.front_door", "on")
	r.Handle("binary_sensor.front_door", "off")
	r.Handle("binary_sensor.front_door", "on")

	// Сразу после шквала показано только первое событие.
	if calls := fake.snapshot(); len(calls) != 1 || calls[0].body != "Дверь открыта" {
		t.Fatalf("сразу после шквала ожидалось только первое уведомление, показано: %+v", calls)
	}

	// По истечении интервала — последнее схлопнутое.
	deadline := time.After(2 * time.Second)
	for len(fake.snapshot()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("отложенное уведомление не показано, всего: %+v", fake.snapshot())
		case <-time.After(10 * time.Millisecond):
		}
	}
	calls := fake.snapshot()
	if len(calls) != 2 {
		t.Fatalf("ожидалось ровно 2 уведомления (первое + последнее), показано %d: %+v", len(calls), calls)
	}
	if calls[1].body != "Дверь открыта" {
		t.Errorf("отложенным должно быть последнее событие ('on'), показано: %+v", calls[1])
	}
}

// TestThrottleIndependentEntities: троттлинг действует на каждую сущность
// независимо — событие другой сущности показывается сразу.
func TestThrottleIndependentEntities(t *testing.T) {
	fake := &fakeNotifier{}
	r := NewRouter(testEntities(), fake, time.Minute)

	r.Handle("binary_sensor.front_door", "on")
	r.Handle("sensor.temp", "20")

	if calls := fake.snapshot(); len(calls) != 2 {
		t.Errorf("события разных сущностей не должны троттлить друг друга, показано %d: %+v", len(calls), calls)
	}
}
