// Атомарная запись файла конфигурации.
// Логика модуля: сохранение конфига из веб-интерфейса (docs/spec.md,
// раздел 3.3) — валидация, сериализация в YAML, запись во временный файл
// рядом с целевым и атомарное переименование, чтобы обрыв записи не оставил
// на диске полфайла. Комментарии ручной правки YAML при этом не сохраняются.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Save валидирует конфиг и атомарно записывает его в path,
// создавая каталог при необходимости.
func Save(cfg *Config, path string) error {
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return fmt.Errorf("невалидная конфигурация не сохранена: %w", err)
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("не удалось сериализовать конфигурацию: %w", err)
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("каталог конфигурации недоступен: %w", err)
	}

	// Временный файл создаётся в том же каталоге: os.Rename атомарен
	// только в пределах одной файловой системы.
	tmp, err := os.CreateTemp(dir, configFileName+".tmp-*")
	if err != nil {
		return fmt.Errorf("не удалось создать временный файл конфигурации: %w", err)
	}
	tmpPath := tmp.Name()
	// При любой ошибке ниже временный файл убирается; после успешного
	// Rename файла уже нет и Remove безвреден.
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("не удалось записать конфигурацию: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("не удалось записать конфигурацию: %w", err)
	}

	// На Windows переименование поверх существующего файла запрещено —
	// целевой файл удаляется непосредственно перед переименованием.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось заменить файл конфигурации: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("не удалось заменить файл конфигурации: %w", err)
	}
	return nil
}
