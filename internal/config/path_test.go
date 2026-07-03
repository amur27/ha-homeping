// Тесты определения пути к конфигурации по умолчанию.
// Логика модуля: проверка построения пути к конфигу по умолчанию —
// как с подменённым базовым каталогом (имитация Windows и macOS),
// так и для реальной текущей ОС через DefaultPath.
package config

import (
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultPathIn проверяет построение пути от ОС-специфичного базового
// каталога: подставляем типичные каталоги Windows и macOS и сверяем результат.
func TestDefaultPathIn(t *testing.T) {
	cases := []struct {
		name    string
		baseDir string
		want    string
	}{
		{
			name:    "windows APPDATA",
			baseDir: `C:\Users\test\AppData\Roaming`,
			want:    filepath.Join(`C:\Users\test\AppData\Roaming`, "homecrier", "config.yaml"),
		},
		{
			name:    "macOS Application Support",
			baseDir: "/Users/test/Library/Application Support",
			want:    filepath.Join("/Users/test/Library/Application Support", "homecrier", "config.yaml"),
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := defaultPathIn(tc.baseDir)
			if got != tc.want {
				t.Errorf("defaultPathIn(%q) = %q, ожидалось %q", tc.baseDir, got, tc.want)
			}
		})
	}
}

// TestDefaultPath проверяет, что для текущей ОС путь строится без ошибки
// и оканчивается на homecrier/config.yaml внутри непустого базового каталога.
func TestDefaultPath(t *testing.T) {
	got, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() вернула ошибку: %v", err)
	}

	wantSuffix := filepath.Join("homecrier", "config.yaml")
	if !strings.HasSuffix(got, wantSuffix) {
		t.Errorf("DefaultPath() = %q, путь должен оканчиваться на %q", got, wantSuffix)
	}
	if strings.TrimSuffix(got, wantSuffix) == "" {
		t.Errorf("DefaultPath() = %q, базовый каталог настроек пуст", got)
	}
}
