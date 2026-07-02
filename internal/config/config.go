// Пакет config отвечает за конфигурацию агента.
// Логика пакета: определение пути к YAML-конфигу по умолчанию для текущей ОС,
// а начиная с task-02 — загрузка файла, строгая валидация полей и получение
// long-lived токена Home Assistant из переменной окружения.
// Формат конфигурации и правила валидации описаны в docs/spec.md, раздел 3.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// appDirName — имя каталога агента внутри стандартного каталога настроек ОС.
const appDirName = "ha-notify-agent"

// configFileName — имя файла конфигурации внутри каталога агента.
const configFileName = "config.yaml"

// DefaultPath возвращает путь к конфигу по умолчанию для текущей ОС:
//   - Windows: %APPDATA%\ha-notify-agent\config.yaml
//   - macOS:   ~/Library/Application Support/ha-notify-agent/config.yaml
//
// Оба варианта покрываются os.UserConfigDir, что гарантирует единообразие
// с разделом 2 спецификации без платформенных ветвлений.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("каталог настроек пользователя недоступен: %w", err)
	}
	return defaultPathIn(base), nil
}

// defaultPathIn строит путь к конфигу от заданного базового каталога настроек.
// Вынесена отдельно, чтобы юнит-тест мог проверить построение пути
// с подменённой ОС-специфичной частью (см. критерии приёмки task-01).
func defaultPathIn(baseDir string) string {
	return filepath.Join(baseDir, appDirName, configFileName)
}
