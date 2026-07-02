// Пакет notify — нативные уведомления операционной системы.
// Логика модуля: интерфейс Notifier абстрагирует системный механизм показа
// (toast на Windows, Notification Center на macOS), реализация Beeep построена
// поверх github.com/gen2brain/beeep. Интерфейс позволяет подменять бэкенд
// в тестах и, при необходимости кнопок-действий, заменить реализацию
// без изменения остального кода (см. ADR-002).
package notify

import "github.com/gen2brain/beeep"

// Notifier — показ одного нативного уведомления ОС.
type Notifier interface {
	Show(title, body string) error
}

// Beeep — реализация Notifier поверх gen2brain/beeep.
type Beeep struct{}

// Show показывает нативное уведомление без иконки — пустой путь
// (beeep сам выбирает системный механизм для текущей ОС;
// nil в качестве иконки beeep v0.11 не принимает).
func (Beeep) Show(title, body string) error {
	return beeep.Notify(title, body, "")
}
