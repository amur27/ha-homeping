// Юнит-тесты пакета secrets.
// Логика модуля: проверка порядка поиска токена (системное хранилище →
// переменная окружения) и различения «не найден» от прочих ошибок.
// Реальное хранилище ОС не трогается — используется mock в памяти.
package secrets

import (
	"errors"
	"testing"
)

// TestResolveOrder проверяет порядок источников: keyring приоритетнее окружения.
func TestResolveOrder(t *testing.T) {
	MockForTests()
	t.Setenv("HA_TOKEN_TEST", "из-окружения")

	// Токена в хранилище нет — берётся переменная окружения.
	got, err := Resolve("HA_TOKEN_TEST")
	if err != nil {
		t.Fatalf("Resolve с токеном в окружении: неожиданная ошибка %v", err)
	}
	if got != "из-окружения" {
		t.Fatalf("Resolve = %q, ожидалось значение из окружения", got)
	}

	// Токен появился в хранилище — он приоритетнее окружения.
	if err := Set("из-хранилища"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err = Resolve("HA_TOKEN_TEST")
	if err != nil {
		t.Fatalf("Resolve с токеном в хранилище: %v", err)
	}
	if got != "из-хранилища" {
		t.Fatalf("Resolve = %q, хранилище должно быть приоритетнее окружения", got)
	}
}

// TestResolveNotFound: токена нет нигде — ошибка распознаётся как ErrNotFound.
func TestResolveNotFound(t *testing.T) {
	MockForTests()

	_, err := Resolve("HA_TOKEN_TEST_MISSING")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve без токена: ожидалась ErrNotFound, получено %v", err)
	}
}

// TestSetGetDelete — базовый цикл работы с хранилищем.
func TestSetGetDelete(t *testing.T) {
	MockForTests()

	if err := Set(""); err == nil {
		t.Fatal("Set с пустым токеном должен возвращать ошибку")
	}
	if err := Set("секрет"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := Get()
	if err != nil || got != "секрет" {
		t.Fatalf("Get = %q, %v; ожидался сохранённый токен", got, err)
	}
	if err := Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := Get(); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get после Delete: ожидалась ErrNotFound, получено %v", err)
	}
	// Повторное удаление отсутствующей записи — не ошибка.
	if err := Delete(); err != nil {
		t.Fatalf("повторный Delete: %v", err)
	}
}
