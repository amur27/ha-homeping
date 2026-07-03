//go:build realkeyring

// Опциональный тест реального системного хранилища учётных данных.
// Логика модуля: проверка цикла Set → Resolve → Delete на настоящем
// Credential Manager (Windows) / Keychain (macOS). В обычные прогоны
// не входит (build-тег realkeyring), запускается вручную:
//
//	go test -tags realkeyring ./internal/secrets -run TestRealStore -v
//
// Тест использует боевые имена сервиса/записи (homeping/ha_token),
// поэтому запускать его на машине с настроенным агентом v2 не следует —
// он перезапишет и удалит сохранённый токен.
package secrets

import (
	"errors"
	"testing"
)

// TestRealStore прогоняет цикл работы с настоящим хранилищем ОС.
func TestRealStore(t *testing.T) {
	// Если запись уже существует — не трогаем её и пропускаем тест,
	// чтобы не уничтожить настоящий токен пользователя.
	if _, err := Get(); err == nil {
		t.Skip("в хранилище уже есть токен homeping/ha_token — тест пропущен, чтобы его не затереть")
	}

	if err := Set("проверочный-токен"); err != nil {
		t.Fatalf("Set в реальное хранилище: %v", err)
	}
	// Гарантированная уборка, даже если проверки ниже упадут.
	defer func() {
		if err := Delete(); err != nil {
			t.Errorf("уборка: Delete из реального хранилища: %v", err)
		}
	}()

	got, err := Resolve("")
	if err != nil {
		t.Fatalf("Resolve из реального хранилища: %v", err)
	}
	if got != "проверочный-токен" {
		t.Fatalf("Resolve = %q, ожидался сохранённый токен", got)
	}

	if err := Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("после Delete ожидалась ErrNotFound, получено %v", err)
	}
}
