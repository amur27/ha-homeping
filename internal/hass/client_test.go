// Тесты WebSocket-клиента Home Assistant.
// Логика модуля: httptest-сервер с WebSocket-эндпоинтом эмулирует протокол HA
// (auth_required → auth → auth_ok/auth_invalid, подписки, события, ping/pong)
// и проверяет сценарии из критериев приёмки task-03: happy path,
// отклонённый токен, молчание на ping, обрыв соединения.
package hass

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// fakeHA — эмулятор WebSocket API Home Assistant для одного соединения.
// Поведение задаётся полями-флагами; необработанные ошибки соединения
// внутри сценария завершают обработчик (это имитирует обрыв).
type fakeHA struct {
	rejectAuth  bool // отвечать auth_invalid вместо auth_ok
	silentPongs bool // не отвечать на ping (для теста таймаута pong)
	// afterSubscribe вызывается после подтверждения всех подписок;
	// получает соединение для отправки событий сценария.
	afterSubscribe func(ctx context.Context, conn *websocket.Conn)
}

// serve обрабатывает одно WebSocket-соединение по протоколу HA.
func (f *fakeHA) serve(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	ctx := r.Context()

	// Фаза аутентификации.
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "auth_required"}); err != nil {
		return
	}
	var auth map[string]any
	if err := wsjson.Read(ctx, conn, &auth); err != nil {
		return
	}
	if f.rejectAuth {
		_ = wsjson.Write(ctx, conn, map[string]string{"type": "auth_invalid"})
		return
	}
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "auth_ok"}); err != nil {
		return
	}

	// Фаза подписок и основной цикл: подтверждаем subscribe_trigger,
	// отвечаем на ping (если не silentPongs).
	subscribed := false
	for {
		var req map[string]any
		if err := wsjson.Read(ctx, conn, &req); err != nil {
			return
		}
		id := req["id"]
		switch req["type"] {
		case "subscribe_trigger":
			resp := map[string]any{"id": id, "type": "result", "success": true}
			if err := wsjson.Write(ctx, conn, resp); err != nil {
				return
			}
			if !subscribed {
				subscribed = true
				if f.afterSubscribe != nil {
					go f.afterSubscribe(ctx, conn)
				}
			}
		case "ping":
			if f.silentPongs {
				continue
			}
			if err := wsjson.Write(ctx, conn, map[string]any{"id": id, "type": "pong"}); err != nil {
				return
			}
		}
	}
}

// startFake поднимает httptest-сервер с эмулятором и возвращает ws-URL.
func startFake(t *testing.T, f *fakeHA) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

// newTestClient создаёт клиента с сокращёнными таймингами протокола.
func newTestClient(url string, entities []string) *Client {
	c := New(url, "test-token", entities)
	c.replyTimeout = 2 * time.Second
	c.pingInterval = 50 * time.Millisecond
	c.pongTimeout = 100 * time.Millisecond
	return c
}

// TestHappyPath: аутентификация, подписка и доставка события потребителю.
func TestHappyPath(t *testing.T) {
	event := map[string]any{
		"id":   1,
		"type": "event",
		"event": map[string]any{
			"variables": map[string]any{
				"trigger": map[string]any{
					"entity_id": "binary_sensor.front_door",
					"to_state":  map[string]any{"state": "on"},
				},
			},
		},
	}
	fake := &fakeHA{
		afterSubscribe: func(ctx context.Context, conn *websocket.Conn) {
			_ = wsjson.Write(ctx, conn, event)
		},
	}
	client := newTestClient(startFake(t, fake), []string{"binary_sensor.front_door"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()

	select {
	case ev := <-client.Events():
		if ev.EntityID != "binary_sensor.front_door" || ev.State != "on" {
			t.Errorf("получено событие %+v, ожидалось binary_sensor.front_door/on", ev)
		}
	case err := <-done:
		t.Fatalf("Run завершился до получения события: %v", err)
	case <-ctx.Done():
		t.Fatal("событие не доставлено за отведённое время")
	}

	cancel() // штатное завершение по контексту
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("после отмены контекста ожидалась context.Canceled, получено: %v", err)
	}
}

// TestAuthInvalid: отклонённый токен должен дать ровно ErrAuthInvalid.
func TestAuthInvalid(t *testing.T) {
	client := newTestClient(startFake(t, &fakeHA{rejectAuth: true}), []string{"a.b"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Run(ctx)
	if !errors.Is(err, ErrAuthInvalid) {
		t.Errorf("ожидалась ErrAuthInvalid, получено: %v", err)
	}
}

// TestPongTimeout: молчание сервера на ping завершает Run с ошибкой
// (не паникой и не зависанием).
func TestPongTimeout(t *testing.T) {
	client := newTestClient(startFake(t, &fakeHA{silentPongs: true}), []string{"a.b"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Run(ctx)
	if err == nil {
		t.Fatal("ожидалась ошибка по таймауту pong, Run завершился без ошибки")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run завис до дедлайна теста вместо таймаута pong: %v", err)
	}
	if !strings.Contains(err.Error(), "ping") {
		t.Errorf("текст ошибки %q не упоминает ping", err.Error())
	}
}

// TestConnectionDrop: обрыв соединения сервером завершает Run с ошибкой.
func TestConnectionDrop(t *testing.T) {
	fake := &fakeHA{
		afterSubscribe: func(ctx context.Context, conn *websocket.Conn) {
			_ = conn.CloseNow() // резкий обрыв после подписки
		},
	}
	client := newTestClient(startFake(t, fake), []string{"a.b"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Run(ctx)
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ожидалась ошибка обрыва соединения, получено: %v", err)
	}
}

// TestOnReady: колбэк готовности вызывается после оформления подписок.
func TestOnReady(t *testing.T) {
	client := newTestClient(startFake(t, &fakeHA{}), []string{"a.b"})
	ready := make(chan struct{})
	client.OnReady = func() { close(ready) }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- client.Run(ctx) }()

	select {
	case <-ready:
	case err := <-done:
		t.Fatalf("Run завершился до вызова OnReady: %v", err)
	case <-ctx.Done():
		t.Fatal("OnReady не вызван за отведённое время")
	}
	cancel()
	<-done
}

// TestEventParsing: событие с незнакомой структурой не роняет клиента.
func TestEventParsing(t *testing.T) {
	c := New("ws://unused", "t", nil)
	// Событие без entity_id — должно быть молча пропущено.
	msg := serverMessage{Type: "event", Event: json.RawMessage(`{"variables":{}}`)}
	c.dispatchEvent(context.Background(), msg)
	select {
	case ev := <-c.Events():
		t.Errorf("событие без entity_id не должно доставляться, получено %+v", ev)
	default:
	}
}
