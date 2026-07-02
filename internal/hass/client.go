// WebSocket-клиент Home Assistant.
// Логика модуля: одно подключение к /api/websocket от dial до обрыва —
// аутентификация по long-lived токену, подписка subscribe_trigger на сущности,
// приём событий смены состояния с передачей их в канал событий, контроль
// живости соединения через ping/pong (docs/spec.md, раздел 4).
// Переподключение реализует супервизор (task-05), не клиент.
package hass

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// ErrAuthInvalid — Home Assistant отверг токен. Переподключение бессмысленно:
// процесс должен завершиться с кодом 3 (docs/spec.md, раздел 8).
var ErrAuthInvalid = errors.New("home assistant отверг токен (auth_invalid)")

// StateEvent — событие смены состояния сущности, передаваемое потребителю.
type StateEvent struct {
	EntityID string // entity_id в Home Assistant
	State    string // новое состояние (trigger.to_state.state)
}

// Тайминги протокола по умолчанию (docs/spec.md, раздел 4).
// Вынесены в поля клиента, чтобы тесты могли их сократить.
const (
	defaultReplyTimeout = 10 * time.Second // ожидание ответа HA на любой запрос
	defaultPingInterval = 50 * time.Second // период отправки ping
	defaultPongTimeout  = 10 * time.Second // ожидание pong после ping
)

// Client — клиент одного WebSocket-соединения с Home Assistant.
type Client struct {
	url      string
	token    string
	entities []string

	events chan StateEvent

	// OnReady вызывается после успешной аутентификации и всех подписок.
	// Используется супервизором (task-05) для сброса бэкоффа
	// и уведомления о восстановлении связи.
	OnReady func()

	// Тайминги протокола; в боевом коде равны default*-константам.
	replyTimeout time.Duration
	pingInterval time.Duration
	pongTimeout  time.Duration
}

// New создаёт клиента для заданного URL, токена и списка entity_id.
func New(url, token string, entityIDs []string) *Client {
	return &Client{
		url:          url,
		token:        token,
		entities:     entityIDs,
		events:       make(chan StateEvent, 16),
		replyTimeout: defaultReplyTimeout,
		pingInterval: defaultPingInterval,
		pongTimeout:  defaultPongTimeout,
	}
}

// Events возвращает канал событий смены состояния. Канал общий для всех
// последовательных подключений клиента и не закрывается при обрыве.
func (c *Client) Events() <-chan StateEvent {
	return c.events
}

// serverMessage — обобщённое входящее сообщение WebSocket API HA.
type serverMessage struct {
	ID      int64           `json:"id"`
	Type    string          `json:"type"`
	Success *bool           `json:"success"`
	Event   json.RawMessage `json:"event"`
}

// eventPayload — полезная нагрузка события подписки subscribe_trigger.
type eventPayload struct {
	Variables struct {
		Trigger struct {
			EntityID string `json:"entity_id"`
			ToState  struct {
				State string `json:"state"`
			} `json:"to_state"`
		} `json:"trigger"`
	} `json:"variables"`
}

// Run выполняет одно подключение: dial, аутентификация, подписки, приём
// событий — и возвращается при обрыве соединения, отмене контекста или
// фатальной ошибке протокола. ErrAuthInvalid ретраить нельзя.
func (c *Client) Run(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, c.replyTimeout)
	conn, _, err := websocket.Dial(dialCtx, c.url, nil)
	cancel()
	if err != nil {
		return fmt.Errorf("не удалось подключиться к %s: %w", c.url, err)
	}
	// Закрытие соединения при любом выходе из Run; статус нормального
	// закрытия HA не требуется — обрыв обрабатывает супервизор.
	defer conn.CloseNow()
	// Состояния HA бывают объёмными (атрибуты) — поднимаем лимит чтения.
	conn.SetReadLimit(1 << 20)

	if err := c.authenticate(ctx, conn); err != nil {
		return err
	}
	if err := c.subscribe(ctx, conn); err != nil {
		return err
	}
	slog.Info("подключено к home assistant, подписки оформлены",
		"url", c.url, "entities", len(c.entities))
	if c.OnReady != nil {
		c.OnReady()
	}

	return c.readLoop(ctx, conn)
}

// readWithTimeout читает одно сообщение с таймаутом ожидания ответа.
func (c *Client) readWithTimeout(ctx context.Context, conn *websocket.Conn) (serverMessage, error) {
	readCtx, cancel := context.WithTimeout(ctx, c.replyTimeout)
	defer cancel()
	var msg serverMessage
	if err := wsjson.Read(readCtx, conn, &msg); err != nil {
		return serverMessage{}, err
	}
	return msg, nil
}

// authenticate проходит фазу аутентификации протокола:
// auth_required → auth(token) → auth_ok | auth_invalid.
func (c *Client) authenticate(ctx context.Context, conn *websocket.Conn) error {
	msg, err := c.readWithTimeout(ctx, conn)
	if err != nil {
		return fmt.Errorf("не дождались auth_required: %w", err)
	}
	if msg.Type != "auth_required" {
		return fmt.Errorf("ожидалось auth_required, получено %q", msg.Type)
	}

	auth := map[string]string{"type": "auth", "access_token": c.token}
	if err := wsjson.Write(ctx, conn, auth); err != nil {
		return fmt.Errorf("не удалось отправить auth: %w", err)
	}

	msg, err = c.readWithTimeout(ctx, conn)
	if err != nil {
		return fmt.Errorf("не дождались результата аутентификации: %w", err)
	}
	switch msg.Type {
	case "auth_ok":
		return nil
	case "auth_invalid":
		return ErrAuthInvalid
	default:
		return fmt.Errorf("неожиданный ответ на auth: %q", msg.Type)
	}
}

// subscribe оформляет подписку subscribe_trigger на каждую сущность
// и дожидается подтверждения success: true для каждой. События,
// пришедшие до последнего подтверждения, обрабатываются сразу.
func (c *Client) subscribe(ctx context.Context, conn *websocket.Conn) error {
	// Монотонный счётчик id команд в рамках соединения; id=1..N — подписки.
	pending := make(map[int64]string, len(c.entities))
	for i, entityID := range c.entities {
		id := int64(i + 1)
		req := map[string]any{
			"id":   id,
			"type": "subscribe_trigger",
			"trigger": map[string]string{
				"platform":  "state",
				"entity_id": entityID,
			},
		}
		if err := wsjson.Write(ctx, conn, req); err != nil {
			return fmt.Errorf("не удалось отправить подписку на %s: %w", entityID, err)
		}
		pending[id] = entityID
	}

	for len(pending) > 0 {
		msg, err := c.readWithTimeout(ctx, conn)
		if err != nil {
			return fmt.Errorf("не дождались подтверждения подписок: %w", err)
		}
		switch msg.Type {
		case "result":
			entityID, ok := pending[msg.ID]
			if !ok {
				continue // ответ на неизвестную команду — игнорируем
			}
			if msg.Success == nil || !*msg.Success {
				return fmt.Errorf("home assistant отклонил подписку на %s", entityID)
			}
			delete(pending, msg.ID)
			slog.Debug("подписка подтверждена", "entity", entityID)
		case "event":
			c.dispatchEvent(ctx, msg)
		}
	}
	return nil
}

// readLoop — основной цикл соединения: приём событий и контроль живости
// через ping/pong. Возвращается при обрыве, молчании на ping или отмене ctx.
func (c *Client) readLoop(ctx context.Context, conn *websocket.Conn) error {
	// Чтение блокирующее, поэтому выносим его в горутину,
	// а цикл управляем select-ом по каналам.
	msgs := make(chan serverMessage)
	readErr := make(chan error, 1)
	go func() {
		for {
			var msg serverMessage
			if err := wsjson.Read(ctx, conn, &msg); err != nil {
				readErr <- err
				return
			}
			select {
			case msgs <- msg:
			case <-ctx.Done():
				return
			}
		}
	}()

	pingTicker := time.NewTicker(c.pingInterval)
	defer pingTicker.Stop()
	// Таймер ожидания pong создаётся остановленным и взводится при отправке ping.
	pongTimer := time.NewTimer(time.Hour)
	pongTimer.Stop()
	defer pongTimer.Stop()

	// id команд ping продолжают нумерацию после подписок.
	nextID := int64(len(c.entities) + 1)
	awaitingPong := false

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case err := <-readErr:
			return fmt.Errorf("соединение с home assistant потеряно: %w", err)

		case msg := <-msgs:
			switch msg.Type {
			case "event":
				c.dispatchEvent(ctx, msg)
			case "pong":
				if awaitingPong {
					awaitingPong = false
					pongTimer.Stop()
				}
			}

		case <-pingTicker.C:
			if awaitingPong {
				continue // предыдущий ping ещё не отвечен — ждём его таймаут
			}
			ping := map[string]any{"id": nextID, "type": "ping"}
			nextID++
			if err := wsjson.Write(ctx, conn, ping); err != nil {
				return fmt.Errorf("не удалось отправить ping: %w", err)
			}
			awaitingPong = true
			pongTimer.Reset(c.pongTimeout)

		case <-pongTimer.C:
			return fmt.Errorf("home assistant не ответил на ping за %s — соединение считается потерянным", c.pongTimeout)
		}
	}
}

// dispatchEvent разбирает событие подписки и отправляет StateEvent потребителю.
// Событие с пустым entity_id (не наш формат) логируется и пропускается.
func (c *Client) dispatchEvent(ctx context.Context, msg serverMessage) {
	var payload eventPayload
	if err := json.Unmarshal(msg.Event, &payload); err != nil {
		slog.Warn("не удалось разобрать событие", "error", err)
		return
	}
	trigger := payload.Variables.Trigger
	if trigger.EntityID == "" {
		slog.Debug("событие без entity_id пропущено")
		return
	}
	ev := StateEvent{EntityID: trigger.EntityID, State: trigger.ToState.State}
	select {
	case c.events <- ev:
	case <-ctx.Done():
	}
}
