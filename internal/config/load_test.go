// Тесты загрузки и валидации конфигурации.
// Логика модуля: проверка всех случаев из критериев приёмки task-02 —
// валидный конфиг из спецификации, строгий режим (неизвестные ключи),
// правила валидации сущностей и URL, значения по умолчанию
// и чтение токена из переменной окружения.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validYAML — конфиг из docs/spec.md, раздел 3.1 (без комментариев).
const validYAML = `
homeassistant:
  url: "ws://homeassistant.local:8123/api/websocket"
  token_env: "HA_TOKEN"
entities:
  - id: binary_sensor.front_door
    name: "Входная дверь"
    states:
      "on": "🚪 Дверь открыта"
      "off": "Дверь закрыта"
  - id: sensor.living_room_temperature
    name: "Температура в гостиной"
    template: "Сейчас {state} °C"
notifications:
  min_interval_sec: 3
  on_disconnect: true
logging:
  level: "info"
`

// writeConfig сохраняет YAML во временный файл и возвращает путь к нему.
func writeConfig(t *testing.T, yamlText string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(yamlText), 0o600); err != nil {
		t.Fatalf("не удалось записать тестовый конфиг: %v", err)
	}
	return path
}

// TestLoadValid проверяет, что эталонный конфиг из спецификации
// загружается без ошибок и поля разобраны корректно.
func TestLoadValid(t *testing.T) {
	cfg, err := Load(writeConfig(t, validYAML))
	if err != nil {
		t.Fatalf("Load() вернула ошибку на валидном конфиге: %v", err)
	}
	if cfg.HomeAssistant.URL != "ws://homeassistant.local:8123/api/websocket" {
		t.Errorf("URL разобран неверно: %q", cfg.HomeAssistant.URL)
	}
	if len(cfg.Entities) != 2 {
		t.Fatalf("ожидалось 2 сущности, получено %d", len(cfg.Entities))
	}
	if got := cfg.Entities[0].States["on"]; got != "🚪 Дверь открыта" {
		t.Errorf("маппинг состояния 'on' разобран неверно: %q", got)
	}
	if cfg.Entities[1].Template != "Сейчас {state} °C" {
		t.Errorf("шаблон разобран неверно: %q", cfg.Entities[1].Template)
	}
	if !cfg.NotifyOnDisconnect() {
		t.Error("on_disconnect: true должен давать NotifyOnDisconnect() == true")
	}
}

// TestLoadDefaults проверяет заполнение значений по умолчанию
// для минимального конфига без необязательных полей.
func TestLoadDefaults(t *testing.T) {
	minimal := `
homeassistant:
  url: "ws://192.168.1.50:8123/api/websocket"
entities:
  - id: binary_sensor.front_door
    states:
      "on": "открыто"
`
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatalf("Load() вернула ошибку на минимальном конфиге: %v", err)
	}
	if cfg.HomeAssistant.TokenEnv != "HA_TOKEN" {
		t.Errorf("token_env по умолчанию должен быть HA_TOKEN, получен %q", cfg.HomeAssistant.TokenEnv)
	}
	if cfg.Notifications.MinIntervalSec != 3 {
		t.Errorf("min_interval_sec по умолчанию должен быть 3, получен %d", cfg.Notifications.MinIntervalSec)
	}
	if !cfg.NotifyOnDisconnect() {
		t.Error("on_disconnect по умолчанию должен быть true")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("logging.level по умолчанию должен быть info, получен %q", cfg.Logging.Level)
	}
	if cfg.Entities[0].Name != "binary_sensor.front_door" {
		t.Errorf("name по умолчанию должен равняться id, получен %q", cfg.Entities[0].Name)
	}
}

// TestLoadErrors перебирает невалидные конфиги из критериев приёмки:
// каждый должен дать ошибку с упоминанием проблемного места.
func TestLoadErrors(t *testing.T) {
	cases := []struct {
		name       string
		yamlText   string
		wantSubstr string // фрагмент, который обязан присутствовать в тексте ошибки
	}{
		{
			name: "неизвестный ключ (строгий режим)",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
  tokenn_env: "HA_TOKEN"
entities:
  - id: a.b
    states: {"on": "x"}
`,
			wantSubstr: "tokenn_env",
		},
		{
			name: "нет ни одной сущности",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
`,
			wantSubstr: "entities",
		},
		{
			name: "states и template одновременно",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
entities:
  - id: a.b
    states: {"on": "x"}
    template: "{state}"
`,
			wantSubstr: "взаимоисключающие",
		},
		{
			name: "ни states, ни template",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
entities:
  - id: a.b
    name: "Датчик"
`,
			wantSubstr: "ровно одно",
		},
		{
			name: "сущность без id",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
entities:
  - name: "Датчик"
    states: {"on": "x"}
`,
			wantSubstr: "id",
		},
		{
			name: "недопустимая схема URL",
			yamlText: `
homeassistant:
  url: "http://ha:8123/api/websocket"
entities:
  - id: a.b
    states: {"on": "x"}
`,
			wantSubstr: "ws://",
		},
		{
			name: "URL не задан",
			yamlText: `
entities:
  - id: a.b
    states: {"on": "x"}
`,
			wantSubstr: "homeassistant.url",
		},
		{
			name: "недопустимый уровень логирования",
			yamlText: `
homeassistant:
  url: "ws://ha:8123/api/websocket"
entities:
  - id: a.b
    states: {"on": "x"}
logging:
  level: "verbose"
`,
			wantSubstr: "logging.level",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, tc.yamlText))
			if err == nil {
				t.Fatal("ожидалась ошибка, но Load() завершилась успешно")
			}
			if !strings.Contains(err.Error(), tc.wantSubstr) {
				t.Errorf("текст ошибки %q не содержит ожидаемый фрагмент %q", err.Error(), tc.wantSubstr)
			}
		})
	}
}

// TestLoadMissingFile проверяет ошибку при отсутствующем файле конфигурации.
func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "нет-такого.yaml"))
	if err == nil {
		t.Fatal("ожидалась ошибка для отсутствующего файла")
	}
}

// TestToken проверяет чтение токена из переменной окружения:
// установленная переменная возвращает значение, пустая — ошибку.
func TestToken(t *testing.T) {
	cfg := &Config{HomeAssistant: HomeAssistant{TokenEnv: "TEST_HA_TOKEN_VAR"}}

	t.Setenv("TEST_HA_TOKEN_VAR", "secret-token-value")
	token, err := cfg.Token()
	if err != nil {
		t.Fatalf("Token() вернула ошибку при установленной переменной: %v", err)
	}
	if token != "secret-token-value" {
		t.Errorf("Token() = %q, ожидалось значение переменной окружения", token)
	}

	t.Setenv("TEST_HA_TOKEN_VAR", "")
	if _, err := cfg.Token(); err == nil {
		t.Error("Token() должна вернуть ошибку при пустой переменной окружения")
	}
}

// TestSlogLevel проверяет преобразование строковых уровней в slog.Level.
func TestSlogLevel(t *testing.T) {
	cases := map[string]string{
		"debug": "DEBUG",
		"info":  "INFO",
		"warn":  "WARN",
		"error": "ERROR",
	}
	for level, want := range cases {
		cfg := &Config{Logging: Logging{Level: level}}
		if got := cfg.SlogLevel().String(); got != want {
			t.Errorf("SlogLevel(%q) = %q, ожидалось %q", level, got, want)
		}
	}
}
