// Маршрутизация событий сущностей в уведомления.
// Логика модуля: Router принимает события смены состояния, строит заголовок
// и текст по правилам конфигурации (states или template), применяет троттлинг
// (docs/spec.md, раздел 5): события одной сущности чаще min_interval_sec
// схлопываются — показывается только последнее по истечении интервала.
package notify

import (
	"log/slog"
	"strings"
	"sync"
	"time"

	"homecrier/internal/config"
)

// message — готовое к показу уведомление.
type message struct {
	title string
	body  string
}

// throttleState — состояние троттлинга одной сущности.
type throttleState struct {
	lastShown time.Time   // момент последнего показанного уведомления
	pending   *message    // последнее схлопнутое событие, ждущее показа
	timer     *time.Timer // таймер отложенного показа pending
}

// Router — потребитель событий: маппинг в тексты и троттлинг.
type Router struct {
	notifier Notifier
	interval time.Duration
	entities map[string]config.Entity

	// now подменяется в тестах для контроля времени.
	now func() time.Time

	mu       sync.Mutex
	throttle map[string]*throttleState
}

// NewRouter создаёт маршрутизатор для сущностей из конфигурации.
func NewRouter(entities []config.Entity, notifier Notifier, minInterval time.Duration) *Router {
	byID := make(map[string]config.Entity, len(entities))
	for _, e := range entities {
		byID[e.ID] = e
	}
	return &Router{
		notifier: notifier,
		interval: minInterval,
		entities: byID,
		now:      time.Now,
		throttle: make(map[string]*throttleState),
	}
}

// Handle обрабатывает событие смены состояния: строит уведомление
// и показывает его сразу либо откладывает по правилам троттлинга.
func (r *Router) Handle(entityID, state string) {
	entity, ok := r.entities[entityID]
	if !ok {
		// HA присылает только подписанные сущности; событие мимо конфига —
		// признак рассинхронизации, фиксируем в лог.
		slog.Debug("событие незнакомой сущности пропущено", "entity", entityID)
		return
	}
	body, ok := buildBody(entity, state)
	if !ok {
		slog.Debug("состояние без маппинга, уведомление не показывается",
			"entity", entityID, "state", state)
		return
	}
	if m := r.enqueue(entityID, message{title: entity.Name, body: body}); m != nil {
		r.show(*m)
	}
}

// buildBody строит текст уведомления по правилам сущности.
// Второй результат false — состояние не порождает уведомления.
func buildBody(e config.Entity, state string) (string, bool) {
	if len(e.States) > 0 {
		body, ok := e.States[state]
		return body, ok
	}
	return strings.ReplaceAll(e.Template, "{state}", state), true
}

// enqueue применяет троттлинг под мьютексом и возвращает уведомление,
// которое нужно показать немедленно (или nil, если показ отложен).
func (r *Router) enqueue(entityID string, m message) *message {
	r.mu.Lock()
	defer r.mu.Unlock()

	st := r.throttle[entityID]
	if st == nil {
		st = &throttleState{}
		r.throttle[entityID] = st
	}

	now := r.now()
	// Вне интервала и без ожидающего показа — показываем сразу.
	if st.timer == nil && now.Sub(st.lastShown) >= r.interval {
		st.lastShown = now
		return &m
	}

	// Внутри интервала — схлопываем: запоминаем последнее событие
	// и взводим таймер на конец интервала (если ещё не взведён).
	st.pending = &m
	if st.timer == nil {
		delay := st.lastShown.Add(r.interval).Sub(now)
		st.timer = time.AfterFunc(delay, func() { r.flush(entityID) })
	}
	return nil
}

// flush показывает отложенное событие сущности по срабатыванию таймера.
func (r *Router) flush(entityID string) {
	r.mu.Lock()
	st := r.throttle[entityID]
	var m *message
	if st != nil {
		m = st.pending
		st.pending = nil
		st.timer = nil
		st.lastShown = r.now()
	}
	r.mu.Unlock()

	if m != nil {
		r.show(*m)
	}
}

// show вызывает системный механизм уведомлений; ошибка показа
// не фатальна для агента и уходит в лог.
func (r *Router) show(m message) {
	if err := r.notifier.Show(m.title, m.body); err != nil {
		slog.Warn("не удалось показать уведомление", "title", m.title, "error", err)
	}
}
