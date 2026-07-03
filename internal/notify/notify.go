// Пакет notify — нативные уведомления операционной системы.
// Логика модуля: интерфейс Notifier абстрагирует системный механизм показа
// (toast на Windows, Notification Center на macOS), реализация Beeep построена
// поверх github.com/gen2brain/beeep. Интерфейс позволяет подменять бэкенд
// в тестах и, при необходимости кнопок-действий, заменить реализацию
// без изменения остального кода (см. ADR-002).
package notify

import (
	"log/slog"
	"strings"

	"github.com/gen2brain/beeep"
)

// Имя приложения для системных механизмов уведомлений
// (на Windows отображается как отправитель тоста).
func init() {
	beeep.AppName = "HomePing"
}

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
	err := beeep.Notify(title, body, "")
	if err != nil && shownViaFallback(err) {
		slog.Debug("уведомление показано запасным путём (PowerShell), ошибка нативного пути подавлена",
			"error", err)
		return nil
	}
	return err
}

// shownViaFallback распознаёт особенность go-toast на Windows: нативный показ
// через WinRT падает на символах вне базовой юникод-плоскости (эмодзи) с ошибкой
// doc.LoadXml, библиотека показывает уведомление запасным путём через PowerShell,
// но возвращает ошибку нативного пути даже при успехе фолбэка
// (bind.go: errors.Join(err, pushPowershell(xml))). Если фолбэк тоже упал,
// в объединённой цепочке появляется его ошибка со словом "powershell" —
// тогда неудача настоящая и подавлять её нельзя.
func shownViaFallback(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "doc.LoadXml") && !strings.Contains(msg, "powershell")
}
