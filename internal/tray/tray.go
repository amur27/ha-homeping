// Пакет tray — иконка агента в трее (Windows) / строке меню (macOS).
// Логика модуля: обёртка над fyne.io/systray — иконка отражает статус
// супервизора (docs/spec.md, раздел 7.1), меню даёт управление агентом
// (раздел 7.2): тестовое уведомление, пауза, открытие и перечитывание
// конфига, выход. Пункт «Настройки…» активируется в task-09 (веб-UI).
// Run обязан вызываться из главной горутины процесса — требование macOS.
package tray

import (
	"embed"
	"fmt"
	"log/slog"
	"os"
	"runtime"

	"fyne.io/systray"

	"homeping/internal/agent"
)

// Иконки статусов, сгенерированные scripts/genicons:
// .ico для Windows, .png для macOS и прочих ОС.
//
//go:embed assets
var assets embed.FS

// Options — зависимости трея.
type Options struct {
	// Agent — супервизор: статусы, пауза, reload, тестовое уведомление.
	Agent *agent.Agent
	// Version — версия для заголовка меню («homeping v1.2.3»).
	Version string
	// ConfigPath — путь к YAML-конфигу для пунктов «Открыть/Перечитать конфиг».
	ConfigPath string
	// SettingsURL возвращает адрес страницы настроек со свежим
	// сессионным токеном (webui.Server.URL) — открывается в браузере.
	SettingsURL func() string
	// RequestExit останавливает агента (отмена его контекста);
	// цикл трея завершает main после остановки агента.
	RequestExit func()
}

// Run запускает цикл трея и блокируется до Quit.
// Вызывать строго из главной горутины (ограничение macOS).
func Run(o Options) {
	systray.Run(func() { onReady(o) }, nil)
}

// Quit завершает цикл трея; безопасен из любой горутины.
func Quit() {
	systray.Quit()
}

// onReady строит меню и подписывается на статусы агента.
func onReady(o Options) {
	title := systray.AddMenuItem("homeping "+o.Version, "")
	title.Disable()
	statusItem := systray.AddMenuItem("Статус: запуск…", "")
	statusItem.Disable()
	systray.AddSeparator()

	settingsItem := systray.AddMenuItem("Настройки…", "открыть страницу настроек в браузере")
	if o.SettingsURL == nil {
		settingsItem.Disable() // веб-интерфейс не запустился
	}
	testItem := systray.AddMenuItem("Тестовое уведомление", "проверка разрешений ОС")
	pauseItem := systray.AddMenuItemCheckbox("Пауза уведомлений", "события не показываются, соединение сохраняется", false)
	systray.AddSeparator()

	openItem := systray.AddMenuItem("Открыть конфиг", o.ConfigPath)
	reloadItem := systray.AddMenuItem("Перечитать конфиг", "применить изменения YAML без перезапуска")
	systray.AddSeparator()

	quitItem := systray.AddMenuItem("Выход", "остановить агента")

	// Индикация статуса: иконка, тултип и строка меню.
	apply := func(s agent.Status) {
		text := statusText(s)
		statusItem.SetTitle("Статус: " + text)
		systray.SetIcon(iconFor(s))
		systray.SetTooltip("HomePing — " + text)
	}
	o.Agent.OnStatusChange(apply)
	apply(o.Agent.Status())

	// Обработка кликов меню. systray потокобезопасен для обновлений,
	// поэтому вся работа идёт в одной фоновой горутине.
	go func() {
		for {
			select {
			case <-settingsItem.ClickedCh:
				OpenSettings(o)

			case <-testItem.ClickedCh:
				if err := o.Agent.TestNotification(); err != nil {
					slog.Warn("не удалось показать пробное уведомление", "error", err)
				}

			case <-pauseItem.ClickedCh:
				if pauseItem.Checked() {
					pauseItem.Uncheck()
					o.Agent.Pause(false)
				} else {
					pauseItem.Check()
					o.Agent.Pause(true)
				}

			case <-openItem.ClickedCh:
				openConfig(o)

			case <-reloadItem.ClickedCh:
				if err := o.Agent.Reload(); err != nil {
					// Ошибка валидации должна быть видна без консоли —
					// показываем уведомлением (в лог её пишет сам Reload).
					showError(o, "Конфиг не применён: "+err.Error())
				}

			case <-quitItem.ClickedCh:
				slog.Info("выход из меню трея")
				o.RequestExit()
			}
		}
	}()
}

// statusText — человекочитаемый статус для меню и тултипа.
func statusText(s agent.Status) string {
	if s.Paused {
		return s.State.String() + ", пауза уведомлений"
	}
	return s.State.String()
}

// iconFor выбирает иконку по статусу: ошибки приоритетнее паузы,
// пауза — приоритетнее индикации связи (docs/spec.md, раздел 7.1).
func iconFor(s agent.Status) []byte {
	switch {
	case s.State == agent.StateAuthError ||
		s.State == agent.StateConfigError ||
		s.State == agent.StateNotConfigured:
		return icon("error")
	case s.Paused:
		return icon("paused")
	case s.State == agent.StateConnected:
		return icon("connected")
	default: // подключение, нет связи
		return icon("disconnected")
	}
}

// icon читает встроенную иконку в формате текущей ОС.
func icon(name string) []byte {
	ext := ".png"
	if runtime.GOOS == "windows" {
		ext = ".ico"
	}
	data, err := assets.ReadFile("assets/" + name + ext)
	if err != nil {
		// Иконки зашиты в бинарник — сюда можно попасть только при
		// рассинхронизации имён; логируем вместо паники.
		slog.Error("встроенная иконка не найдена", "name", name, "error", err)
		return nil
	}
	return data
}

// OpenSettings открывает страницу настроек в браузере по умолчанию.
// Экспортирована: main вызывает её сам при первом запуске без конфига
// (docs/spec.md, раздел 2).
func OpenSettings(o Options) {
	if o.SettingsURL == nil {
		return
	}
	if err := openFile(o.SettingsURL()); err != nil {
		slog.Warn("не удалось открыть браузер", "error", err)
		showError(o, "Не удалось открыть браузер с настройками: "+err.Error())
	}
}

// openConfig открывает YAML в приложении по умолчанию.
func openConfig(o Options) {
	if _, err := os.Stat(o.ConfigPath); err != nil {
		showError(o, "Файл конфигурации ещё не создан: "+o.ConfigPath)
		return
	}
	if err := openFile(o.ConfigPath); err != nil {
		slog.Warn("не удалось открыть конфиг", "error", err)
		showError(o, "Не удалось открыть конфиг: "+err.Error())
	}
}

// showError показывает пользователю ошибку системным уведомлением —
// у трей-агента нет консоли, уведомление единственный заметный канал.
func showError(o Options, text string) {
	if err := o.Agent.Notifier.Show("HomePing", fmt.Sprintf("⚠️ %s", text)); err != nil {
		slog.Warn("не удалось показать уведомление об ошибке", "error", err)
	}
}
