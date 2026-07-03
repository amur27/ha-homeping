// Пакет agent — супервизор верхнего уровня.
// Логика модуля: владеет жизненным циклом агента — загрузка конфига и токена,
// запуск сессии (WebSocket-клиент + маршрутизатор уведомлений + супервизор
// соединения из internal/hass), hot-reload конфига без перезапуска процесса,
// пауза уведомлений и потокобезопасный статус с подпиской на изменения для
// трея и веб-интерфейса (docs/spec.md, разделы 4, 5, 10, 12).
// В режиме FailFast (headless, -no-tray) ошибки конфигурации и токена
// фатальны, как в v1; в трей-режиме агент остаётся жить и ждёт исправления
// настроек через Reload.
package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"homeping/internal/config"
	"homeping/internal/hass"
	"homeping/internal/notify"
	"homeping/internal/secrets"
)

// State — состояние агента для индикации в трее и веб-интерфейсе.
type State int

const (
	// StateNotConfigured — нет валидного токена: агент ждёт настройки.
	StateNotConfigured State = iota
	// StateConfigError — конфиг отсутствует или невалиден: агент ждёт исправления.
	StateConfigError
	// StateConnecting — идёт подключение (или переподключение с бэкоффом).
	StateConnecting
	// StateConnected — аутентификация пройдена, подписки активны.
	StateConnected
	// StateDisconnected — связь потеряна, идёт переподключение.
	StateDisconnected
	// StateAuthError — HA отверг токен: агент ждёт нового токена.
	StateAuthError
)

// String возвращает человекочитаемое описание состояния (для трея и логов).
func (s State) String() string {
	switch s {
	case StateNotConfigured:
		return "не настроен"
	case StateConfigError:
		return "ошибка конфига"
	case StateConnecting:
		return "подключение…"
	case StateConnected:
		return "подключён"
	case StateDisconnected:
		return "нет связи"
	case StateAuthError:
		return "ошибка токена"
	default:
		return "неизвестно"
	}
}

// Status — снимок состояния агента для подписчиков.
type Status struct {
	State  State
	Paused bool
	// Detail — текст ошибки для состояний NotConfigured/ConfigError/AuthError.
	Detail string
}

// ErrConfig помечает фатальные ошибки конфигурации/токена в режиме FailFast —
// процесс завершается кодом 2 (docs/spec.md, раздел 12).
var ErrConfig = errors.New("ошибка конфигурации")

// downAfter — порог простоя до уведомления «HA недоступен» (спека, раздел 5).
const downAfter = 30 * time.Second

// Agent — супервизор верхнего уровня.
type Agent struct {
	// ConfigPath — путь к YAML-конфигу (источник истины для Reload).
	ConfigPath string
	// Notifier — системный механизм уведомлений (в тестах — фейк).
	Notifier notify.Notifier
	// FailFast — headless-режим: ошибки конфига/токена фатальны (коды 2/3).
	FailFast bool
	// OnConfig вызывается после каждой успешной загрузки конфига
	// (запуск и reload) — например, для смены уровня логирования.
	OnConfig func(*config.Config)

	// resolveToken и runSession подменяются в тестах; по умолчанию —
	// secrets.Resolve и реальная сессия с WebSocket-клиентом.
	resolveToken func(cfg *config.Config) (string, error)
	runSession   func(ctx context.Context, cfg *config.Config, token string) error

	mu            sync.Mutex
	status        Status
	listeners     []func(Status)
	cancelSession context.CancelFunc
	paused        bool

	// reloadCh буферизован на один сигнал: Reload во время ожидания будит
	// waitReload, во время сессии — помечает перезапуск после её отмены.
	reloadCh chan struct{}
	initOnce sync.Once
}

// init лениво инициализирует внутренние поля (Agent создаётся литералом).
func (a *Agent) init() {
	a.initOnce.Do(func() {
		a.reloadCh = make(chan struct{}, 1)
		if a.resolveToken == nil {
			a.resolveToken = func(cfg *config.Config) (string, error) {
				return secrets.Resolve(cfg.HomeAssistant.TokenEnv)
			}
		}
		if a.runSession == nil {
			a.runSession = a.defaultSession
		}
	})
}

// Run — основной цикл агента: конфиг → токен → сессия; после отмены сессии
// (Reload) всё перечитывается заново. Возвращается при отмене ctx (nil)
// либо, в режиме FailFast, с фатальной ошибкой (ErrConfig, hass.ErrAuthInvalid).
func (a *Agent) Run(ctx context.Context) error {
	a.init()
	for {
		if ctx.Err() != nil {
			return nil
		}
		// Сигнал Reload, оставшийся от отменённой сессии, уже отработан
		// самим заходом на новый круг — убираем его, чтобы не сработал дважды.
		select {
		case <-a.reloadCh:
		default:
		}

		cfg, err := config.Load(a.ConfigPath)
		if err != nil {
			if a.FailFast {
				return fmt.Errorf("%w: %v", ErrConfig, err)
			}
			slog.Error("конфигурация не загружена", "error", err)
			a.setStatus(StateConfigError, err.Error())
			if !a.waitReload(ctx) {
				return nil
			}
			continue
		}
		if a.OnConfig != nil {
			a.OnConfig(cfg)
		}

		token, err := a.resolveToken(cfg)
		if err != nil {
			if a.FailFast {
				return fmt.Errorf("%w: %v", ErrConfig, err)
			}
			slog.Warn("токен не найден, агент ждёт настройки", "error", err)
			a.setStatus(StateNotConfigured, err.Error())
			if !a.waitReload(ctx) {
				return nil
			}
			continue
		}

		sessionCtx, cancel := context.WithCancel(ctx)
		a.mu.Lock()
		a.cancelSession = cancel
		a.mu.Unlock()
		a.setStatus(StateConnecting, "")

		err = a.runSession(sessionCtx, cfg, token)
		cancel()
		a.mu.Lock()
		a.cancelSession = nil
		a.mu.Unlock()

		if errors.Is(err, hass.ErrAuthInvalid) {
			if a.FailFast {
				return err
			}
			slog.Error("home assistant отверг токен, агент ждёт нового токена")
			a.notify("HomePing", "Токен отвергнут Home Assistant — откройте настройки и введите новый")
			a.setStatus(StateAuthError, err.Error())
			if !a.waitReload(ctx) {
				return nil
			}
			continue
		}
		if ctx.Err() != nil {
			slog.Info("получен сигнал завершения, агент останавливается")
			return nil
		}
		// Сессия отменена Reload-ом — новый круг перечитает конфиг и токен.
		slog.Info("перезапуск с новой конфигурацией")
	}
}

// Reload перечитывает конфигурацию и применяет её без перезапуска процесса.
// Невалидный конфиг не прерывает текущую работу: возвращается ошибка,
// агент продолжает на прежней конфигурации (docs/spec.md, раздел 10).
func (a *Agent) Reload() error {
	a.init()
	// Валидация до перезапуска сессии: работающую конфигурацию
	// нельзя ронять ради невалидной новой.
	if _, err := config.Load(a.ConfigPath); err != nil {
		slog.Warn("reload отклонён: конфигурация невалидна", "error", err)
		return err
	}

	select {
	case a.reloadCh <- struct{}{}:
	default: // сигнал уже стоит — второй не нужен
	}
	a.mu.Lock()
	cancel := a.cancelSession
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

// Pause включает/выключает паузу уведомлений: события обрабатываются
// и логируются, но уведомления не показываются (docs/spec.md, раздел 5).
func (a *Agent) Pause(on bool) {
	a.mu.Lock()
	a.paused = on
	st := a.status
	st.Paused = on
	a.status = st
	listeners := append([]func(Status){}, a.listeners...)
	a.mu.Unlock()

	slog.Info("пауза уведомлений", "включена", on)
	for _, l := range listeners {
		l(st)
	}
}

// TestNotification показывает пробное уведомление (пункт меню трея и веб-UI).
// Пауза намеренно игнорируется: пользователь запросил показ явно.
func (a *Agent) TestNotification() error {
	return a.Notifier.Show("HomePing", "Агент работает — уведомления настроены правильно")
}

// Status возвращает текущий снимок состояния.
func (a *Agent) Status() Status {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.status
}

// OnStatusChange подписывает слушателя на изменения состояния
// (трей, веб-интерфейс). Слушатель вызывается вне мьютекса.
func (a *Agent) OnStatusChange(l func(Status)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.listeners = append(a.listeners, l)
}

// setStatus обновляет состояние и оповещает подписчиков.
func (a *Agent) setStatus(s State, detail string) {
	a.mu.Lock()
	st := Status{State: s, Paused: a.paused, Detail: detail}
	a.status = st
	listeners := append([]func(Status){}, a.listeners...)
	a.mu.Unlock()

	for _, l := range listeners {
		l(st)
	}
}

// waitReload блокируется до сигнала Reload (true) или отмены ctx (false).
func (a *Agent) waitReload(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return false
	case <-a.reloadCh:
		return true
	}
}

// notify показывает служебное уведомление агента с учётом паузы.
func (a *Agent) notify(title, body string) {
	if a.isPaused() {
		slog.Debug("пауза: уведомление подавлено", "title", title, "body", body)
		return
	}
	if err := a.Notifier.Show(title, body); err != nil {
		slog.Warn("не удалось показать уведомление", "title", title, "error", err)
	}
}

// isPaused сообщает, включена ли пауза уведомлений.
func (a *Agent) isPaused() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.paused
}

// pauseGate — обёртка Notifier, подавляющая показ при включённой паузе.
// Через неё проходят и события сущностей (Router), и уведомления об обрыве.
type pauseGate struct{ a *Agent }

// Show показывает уведомление, если пауза не включена.
func (g pauseGate) Show(title, body string) error {
	if g.a.isPaused() {
		slog.Debug("пауза: уведомление подавлено", "title", title, "body", body)
		return nil
	}
	return g.a.Notifier.Show(title, body)
}

// defaultSession — одна сессия с реальным клиентом Home Assistant:
// от загрузки конфига до отмены контекста или фатальной ошибки токена.
// Переподключения при сетевых сбоях остаются внутри сессии (hass.Supervisor).
func (a *Agent) defaultSession(ctx context.Context, cfg *config.Config, token string) error {
	slog.Info("запуск сессии",
		"ha_url", cfg.HomeAssistant.URL, "entities", len(cfg.Entities))

	entityIDs := make([]string, len(cfg.Entities))
	for i, e := range cfg.Entities {
		entityIDs[i] = e.ID
	}
	client := hass.New(cfg.HomeAssistant.URL, token, entityIDs)

	// Потребитель событий: маршрутизатор строит тексты, троттлит
	// и показывает уведомления через шлюз паузы.
	router := notify.NewRouter(cfg.Entities, pauseGate{a},
		time.Duration(cfg.Notifications.MinIntervalSec)*time.Second)
	go func() {
		for {
			select {
			case ev := <-client.Events():
				slog.Debug("событие", "entity", ev.EntityID, "state", ev.State)
				router.Handle(ev.EntityID, ev.State)
			case <-ctx.Done():
				return
			}
		}
	}()

	sup := &hass.Supervisor{
		Run: func(runCtx context.Context, onReady func()) error {
			client.OnReady = func() {
				a.setStatus(StateConnected, "")
				onReady()
			}
			err := client.Run(runCtx)
			// Обрыв (не отмена и не плохой токен) — статус «нет связи»
			// на время бэкоффа и переподключения.
			if runCtx.Err() == nil && !errors.Is(err, hass.ErrAuthInvalid) {
				a.setStatus(StateDisconnected, "")
			}
			return err
		},
		Backoff:   hass.NewBackoff(),
		DownAfter: downAfter,
	}
	if cfg.NotifyOnDisconnect() {
		gate := pauseGate{a}
		sup.OnDown = func() {
			if err := gate.Show("HomePing", "⚠️ Home Assistant недоступен"); err != nil {
				slog.Warn("не удалось показать уведомление о простое", "error", err)
			}
		}
		sup.OnUp = func() {
			if err := gate.Show("HomePing", "✅ Связь с Home Assistant восстановлена"); err != nil {
				slog.Warn("не удалось показать уведомление о восстановлении", "error", err)
			}
		}
	}

	return sup.Loop(ctx)
}
