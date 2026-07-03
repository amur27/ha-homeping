// Определение пути к файлу конфигурации по умолчанию.
// Логика модуля: построение стандартного пути к config.yaml для текущей ОС
// поверх os.UserConfigDir (Windows: %APPDATA%, macOS: ~/Library/Application Support)
// в соответствии с docs/spec.md, раздел 2.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// appDirName — имя каталога агента внутри стандартного каталога настроек ОС.
const appDirName = "homecrier"

// configFileName — имя файла конфигурации внутри каталога агента.
const configFileName = "config.yaml"

// DefaultPath возвращает путь к конфигу по умолчанию для текущей ОС:
//   - Windows: %APPDATA%\homecrier\config.yaml
//   - macOS:   ~/Library/Application Support/homecrier/config.yaml
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
