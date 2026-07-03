// Пакет config отвечает за конфигурацию агента.
// Логика модуля: структуры конфигурации, загрузка YAML-файла в строгом режиме
// (неизвестные ключи — ошибка), заполнение значений по умолчанию, валидация
// по правилам docs/spec.md (раздел 3.2) и атомарная запись конфига (save.go).
// Токен в файле не хранится — его получение реализует пакет secrets.
package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// Значения по умолчанию для необязательных полей конфигурации.
const (
	defaultTokenEnv       = "HA_TOKEN"
	defaultMinIntervalSec = 3
	defaultLogLevel       = "info"
)

// Config — корневая структура файла конфигурации (docs/spec.md, раздел 3.1).
// JSON-теги повторяют YAML: структура используется и как тело API
// веб-интерфейса настроек (docs/spec.md, раздел 8.3).
type Config struct {
	HomeAssistant HomeAssistant `yaml:"homeassistant" json:"homeassistant"`
	Entities      []Entity      `yaml:"entities" json:"entities"`
	Notifications Notifications `yaml:"notifications" json:"notifications"`
	Logging       Logging       `yaml:"logging" json:"logging"`
}

// HomeAssistant — параметры подключения к Home Assistant.
type HomeAssistant struct {
	// URL WebSocket API; допустимы только схемы ws:// и wss://.
	URL string `yaml:"url" json:"url"`
	// TokenEnv — имя переменной окружения с long-lived токеном
	// (резервный источник после системного хранилища).
	TokenEnv string `yaml:"token_env" json:"token_env,omitempty"`
}

// Entity — наблюдаемая сущность и правила построения текста уведомления.
// Должно быть задано ровно одно из полей States или Template.
type Entity struct {
	// ID — entity_id в Home Assistant, например binary_sensor.front_door.
	ID string `yaml:"id" json:"id"`
	// Name — заголовок уведомления; по умолчанию равен ID.
	Name string `yaml:"name" json:"name"`
	// States — маппинг «состояние → текст уведомления»;
	// состояние без записи в маппинге уведомления не порождает.
	States map[string]string `yaml:"states,omitempty" json:"states,omitempty"`
	// Template — шаблон текста для любого состояния; {state} заменяется значением.
	Template string `yaml:"template,omitempty" json:"template,omitempty"`
}

// Notifications — общие настройки показа уведомлений.
type Notifications struct {
	// MinIntervalSec — троттлинг: не чаще одного уведомления на сущность за N секунд.
	MinIntervalSec int `yaml:"min_interval_sec" json:"min_interval_sec"`
	// OnDisconnect — уведомлять о потере/восстановлении связи с HA.
	// Указатель различает «не задано» (по умолчанию true) и явное false.
	OnDisconnect *bool `yaml:"on_disconnect" json:"on_disconnect"`
}

// Logging — настройки логирования.
type Logging struct {
	// Level — уровень: debug | info | warn | error.
	Level string `yaml:"level" json:"level"`
	// File — путь к файлу логов; пусто — путь по умолчанию для текущей ОС
	// (docs/spec.md, раздел 11).
	File string `yaml:"file,omitempty" json:"file,omitempty"`
}

// Load читает YAML-файл конфигурации в строгом режиме, заполняет значения
// по умолчанию и валидирует результат. Любая ошибка означает невалидную
// конфигурацию (код выхода 2 на уровне процесса).
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("файл конфигурации недоступен: %w", err)
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	// Строгий режим: опечатка в имени ключа — ошибка, а не молчаливое игнорирование.
	dec.KnownFields(true)

	var cfg Config
	if err := dec.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("ошибка разбора YAML в %s: %w", path, err)
	}

	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("невалидная конфигурация %s: %w", path, err)
	}
	return &cfg, nil
}

// applyDefaults заполняет необязательные поля значениями по умолчанию.
func (c *Config) applyDefaults() {
	if c.HomeAssistant.TokenEnv == "" {
		c.HomeAssistant.TokenEnv = defaultTokenEnv
	}
	if c.Notifications.MinIntervalSec == 0 {
		c.Notifications.MinIntervalSec = defaultMinIntervalSec
	}
	if c.Notifications.OnDisconnect == nil {
		v := true
		c.Notifications.OnDisconnect = &v
	}
	if c.Logging.Level == "" {
		c.Logging.Level = defaultLogLevel
	}
	for i := range c.Entities {
		if c.Entities[i].Name == "" {
			c.Entities[i].Name = c.Entities[i].ID
		}
	}
}

// validate проверяет конфигурацию по правилам docs/spec.md, раздел 3.2.
func (c *Config) validate() error {
	if c.HomeAssistant.URL == "" {
		return fmt.Errorf("homeassistant.url: обязательное поле не задано")
	}
	u, err := url.Parse(c.HomeAssistant.URL)
	if err != nil {
		return fmt.Errorf("homeassistant.url: не удалось разобрать URL: %w", err)
	}
	if u.Scheme != "ws" && u.Scheme != "wss" {
		return fmt.Errorf("homeassistant.url: допустимы только схемы ws:// и wss://, получена %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("homeassistant.url: в URL отсутствует адрес сервера")
	}

	if len(c.Entities) == 0 {
		return fmt.Errorf("entities: нужна хотя бы одна сущность")
	}
	for i, e := range c.Entities {
		if e.ID == "" {
			return fmt.Errorf("entities[%d].id: обязательное поле не задано", i)
		}
		hasStates := len(e.States) > 0
		hasTemplate := e.Template != ""
		if hasStates && hasTemplate {
			return fmt.Errorf("entities[%d] (%s): поля states и template взаимоисключающие, задано оба", i, e.ID)
		}
		if !hasStates && !hasTemplate {
			return fmt.Errorf("entities[%d] (%s): нужно задать ровно одно из полей states или template", i, e.ID)
		}
	}

	if c.Notifications.MinIntervalSec < 0 {
		return fmt.Errorf("notifications.min_interval_sec: значение не может быть отрицательным")
	}

	if _, err := parseLevel(c.Logging.Level); err != nil {
		return fmt.Errorf("logging.level: %w", err)
	}
	return nil
}

// NotifyOnDisconnect сообщает, нужно ли уведомлять о потере/восстановлении
// связи с HA (после applyDefaults указатель всегда ненулевой).
func (c *Config) NotifyOnDisconnect() bool {
	return c.Notifications.OnDisconnect != nil && *c.Notifications.OnDisconnect
}

// SlogLevel возвращает уровень логирования как slog.Level.
// Вызывается после успешной валидации, поэтому уровень заведомо корректен.
func (c *Config) SlogLevel() slog.Level {
	lvl, _ := parseLevel(c.Logging.Level)
	return lvl
}

// parseLevel преобразует строковый уровень логирования в slog.Level.
func parseLevel(s string) (slog.Level, error) {
	switch s {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("недопустимый уровень %q, допустимы: debug, info, warn, error", s)
	}
}
