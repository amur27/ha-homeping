// Интеграционный тест веб-интерфейса на реальном сокете.
// Логика модуля: полный путь пользователя без браузера — Start() на
// 127.0.0.1 с эфемерным портом, заход по ссылке из URL() (?auth=…),
// получение страницы и cookie, работа с API по cookie, Close().
package webui

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"testing"

	"homeping/internal/agent"
	"homeping/internal/config"
	"homeping/internal/secrets"
)

// TestServerLive повторяет путь «клик в трее → страница → API».
func TestServerLive(t *testing.T) {
	secrets.MockForTests()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := config.Save(config.Starter(), path); err != nil {
		t.Fatal(err)
	}

	s := &Server{
		Agent:      &agent.Agent{ConfigPath: path, Notifier: &fakeNotifier{}},
		Version:    "live-test",
		ConfigPath: path,
	}
	if err := s.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer s.Close()

	url := s.URL()
	if !strings.HasPrefix(url, "http://127.0.0.1:") || !strings.Contains(url, "?auth=") {
		t.Fatalf("URL() = %q — ожидался loopback-адрес со ссылкой авторизации", url)
	}

	// Браузер: заход по ссылке из трея, cookie запоминается.
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("заход по ссылке: %v", err)
	}
	page, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(page), "HomePing") {
		t.Fatalf("страница не отдалась: статус %d", resp.StatusCode)
	}

	// Дальше API работает по cookie, без ?auth в адресе.
	resp, err = client.Get(strings.Split(url, "?")[0] + "api/status")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "live-test") {
		t.Fatalf("статус по cookie: %d, тело %s", resp.StatusCode, body)
	}

	// Другой клиент без cookie по тому же адресу — 403.
	resp, err = http.Get(strings.Split(url, "?")[0] + "api/status")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("без cookie: статус %d, ожидался 403", resp.StatusCode)
	}
}
