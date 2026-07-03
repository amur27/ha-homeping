// Пакет secrets отвечает за хранение токена Home Assistant.
// Логика модуля: основное хранилище — системное (Windows: Credential Manager,
// macOS: Keychain) через go-keyring; резервный источник — переменная окружения,
// имя которой задаётся в конфиге (token_env). Порядок поиска и запрет на
// логирование токена — docs/spec.md, раздел 9. Токен, сохранённый из веб-UI,
// пишется только в системное хранилище.
package secrets

import (
	"errors"
	"fmt"
	"os"

	"github.com/zalando/go-keyring"
)

// Имя сервиса и учётной записи в системном хранилище (docs/spec.md, раздел 9).
const (
	service = "homeping"
	account = "ha_token"
)

// ErrNotFound — токен не найден ни в одном источнике. Отличается от прочих
// ошибок хранилища: «не найден» — ожидаемое состояние ненастроенного агента,
// остальное — сбои, которые нужно показывать как ошибки.
var ErrNotFound = errors.New("токен home assistant не найден")

// Get возвращает токен из системного хранилища.
// Отсутствие записи транслируется в ErrNotFound.
func Get() (string, error) {
	token, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("системное хранилище учётных данных недоступно: %w", err)
	}
	return token, nil
}

// Set сохраняет токен в системное хранилище (перезаписывая существующий).
func Set(token string) error {
	if token == "" {
		return fmt.Errorf("пустой токен не сохраняется")
	}
	if err := keyring.Set(service, account, token); err != nil {
		return fmt.Errorf("не удалось сохранить токен в системное хранилище: %w", err)
	}
	return nil
}

// Delete удаляет токен из системного хранилища; отсутствие записи — не ошибка.
func Delete() error {
	err := keyring.Delete(service, account)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return fmt.Errorf("не удалось удалить токен из системного хранилища: %w", err)
	}
	return nil
}

// Resolve ищет токен по порядку из спецификации: системное хранилище →
// переменная окружения tokenEnv (если имя непустое). Возвращает ErrNotFound
// (обёрнутый в понятное сообщение), если токена нет нигде.
func Resolve(tokenEnv string) (string, error) {
	token, err := Get()
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}
	if tokenEnv != "" {
		if v := os.Getenv(tokenEnv); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("%w: добавьте его через настройки агента либо задайте переменную окружения %s", ErrNotFound, tokenEnv)
}

// MockForTests подменяет системное хранилище на хранилище в памяти —
// только для юнит-тестов, чтобы не трогать реальный Credential Manager/Keychain.
func MockForTests() {
	keyring.MockInit()
}
