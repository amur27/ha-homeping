// Юнит-тесты веб-интерфейса настроек.
// Логика модуля: проверка авторизации (403 без сессионного токена, перенос
// токена из query в cookie), сохранения конфига через PUT (422 на невалидных
// данных, файл не трогается), эквивалентности GET /api/config содержимому
// диска, сохранения токена HA (mock-хранилище) и того, что токен никогда
// не возвращается клиентом. Сеть HA не используется — агент не запускается.
package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"homeping/internal/agent"
	"homeping/internal/config"
	"homeping/internal/secrets"
)

// fakeNotifier считает показанные уведомления.
type fakeNotifier struct{ shown int }

func (f *fakeNotifier) Show(title, body string) error { f.shown++; return nil }

// newTestServer собирает сервер с временным конфигом и mock-хранилищем.
func newTestServer(t *testing.T) (*Server, *httptest.Server, string) {
	t.Helper()
	secrets.MockForTests()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(config.Starter(), path); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		Agent:      &agent.Agent{ConfigPath: path, Notifier: &fakeNotifier{}},
		Version:    "test",
		ConfigPath: path,
		authToken:  "test-session-token",
	}
	ts := httptest.NewServer(s.routes())
	t.Cleanup(ts.Close)
	return s, ts, path
}

// authedClient возвращает клиента с cookie сессии.
func authedRequest(t *testing.T, ts *httptest.Server, method, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(&http.Cookie{Name: cookieName, Value: "test-session-token"})
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// TestAuth: без токена — 403; с токеном в query — 200 и cookie;
// с чужим токеном — 403.
func TestAuth(t *testing.T) {
	_, ts, _ := newTestServer(t)

	resp, err := http.Get(ts.URL + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("без токена: статус %d, ожидался 403", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/status?auth=чужой-токен")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("с чужим токеном: статус %d, ожидался 403", resp.StatusCode)
	}

	resp, err = http.Get(ts.URL + "/api/status?auth=test-session-token")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("с верным токеном: статус %d, ожидался 200", resp.StatusCode)
	}
	var gotCookie bool
	for _, c := range resp.Cookies() {
		if c.Name == cookieName && c.Value == "test-session-token" {
			gotCookie = c.HttpOnly && c.SameSite == http.SameSiteStrictMode
		}
	}
	if !gotCookie {
		t.Fatal("cookie сессии не установлена или без HttpOnly/SameSite=Strict")
	}
}

// TestConfigRoundtrip: PUT валидного конфига пишет YAML на диск,
// GET возвращает то же самое.
func TestConfigRoundtrip(t *testing.T) {
	_, ts, path := newTestServer(t)

	body := `{
		"homeassistant": {"url": "ws://ha.local:8123/api/websocket"},
		"entities": [{"id": "sensor.temp", "name": "Температура", "template": "Сейчас {state} °C"}],
		"notifications": {"min_interval_sec": 5, "on_disconnect": false},
		"logging": {"level": "debug"}
	}`
	resp := authedRequest(t, ts, http.MethodPut, "/api/config", body)
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("PUT валидного конфига: статус %d, тело %s", resp.StatusCode, b)
	}

	// Файл на диске обновлён и валиден.
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("конфиг на диске после PUT: %v", err)
	}
	if cfg.Entities[0].Template != "Сейчас {state} °C" || cfg.Notifications.MinIntervalSec != 5 {
		t.Fatalf("конфиг на диске не совпадает с отправленным: %+v", cfg)
	}

	// GET возвращает то же представление.
	resp = authedRequest(t, ts, http.MethodGet, "/api/config", "")
	var got config.Config
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.HomeAssistant.URL != "ws://ha.local:8123/api/websocket" || len(got.Entities) != 1 {
		t.Fatalf("GET /api/config вернул не то, что сохранено: %+v", got)
	}
}

// TestPutInvalid: невалидные данные — 422 с русским описанием,
// файл на диске не изменяется.
func TestPutInvalid(t *testing.T) {
	_, ts, path := newTestServer(t)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]string{
		"пустые сущности":  `{"homeassistant":{"url":"ws://x/api/websocket"},"entities":[],"notifications":{"min_interval_sec":3},"logging":{"level":"info"}}`,
		"плохая схема url": `{"homeassistant":{"url":"http://x"},"entities":[{"id":"a","template":"{state}"}],"notifications":{"min_interval_sec":3},"logging":{"level":"info"}}`,
		"неизвестное поле": `{"сюрприз": 1}`,
		"мусор":            `{{{`,
	}
	for name, body := range cases {
		resp := authedRequest(t, ts, http.MethodPut, "/api/config", body)
		if resp.StatusCode != http.StatusUnprocessableEntity {
			t.Errorf("%s: статус %d, ожидался 422", name, resp.StatusCode)
		}
		var e struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&e); err != nil || e.Error == "" {
			t.Errorf("%s: нет описания ошибки в ответе", name)
		}
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("невалидный PUT изменил файл конфигурации")
	}
}

// TestToken: токен сохраняется в хранилище, статус показывает «задан»,
// но само значение не появляется ни в одном ответе API.
func TestToken(t *testing.T) {
	_, ts, _ := newTestServer(t)

	// До сохранения статус говорит «токен не задан».
	resp := authedRequest(t, ts, http.MethodGet, "/api/status", "")
	var st struct {
		TokenSet bool `json:"token_set"`
	}
	json.NewDecoder(resp.Body).Decode(&st)
	if st.TokenSet {
		t.Fatal("token_set=true до сохранения токена")
	}

	// Пустой токен — 422.
	resp = authedRequest(t, ts, http.MethodPost, "/api/token", `{"token":"  "}`)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("пустой токен: статус %d, ожидался 422", resp.StatusCode)
	}

	// Настоящий токен сохраняется в хранилище.
	resp = authedRequest(t, ts, http.MethodPost, "/api/token", `{"token":"секретный-токен"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("сохранение токена: статус %d", resp.StatusCode)
	}
	if got, err := secrets.Get(); err != nil || got != "секретный-токен" {
		t.Fatalf("токен не попал в хранилище: %q, %v", got, err)
	}

	// Значение токена не возвращается ни статусом, ни конфигом.
	for _, p := range []string{"/api/status", "/api/config"} {
		resp = authedRequest(t, ts, http.MethodGet, p, "")
		b, _ := io.ReadAll(resp.Body)
		if strings.Contains(string(b), "секретный-токен") {
			t.Fatalf("%s вернул значение токена", p)
		}
	}
}

// TestTestNotification: POST /api/test показывает уведомление.
func TestTestNotification(t *testing.T) {
	s, ts, _ := newTestServer(t)

	resp := authedRequest(t, ts, http.MethodPost, "/api/test", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("тестовое уведомление: статус %d", resp.StatusCode)
	}
	if n := s.Agent.Notifier.(*fakeNotifier).shown; n != 1 {
		t.Fatalf("показано уведомлений: %d, ожидалось 1", n)
	}
}
