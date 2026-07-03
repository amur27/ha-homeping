// Открытие файла приложением по умолчанию — Windows.
// Логика модуля: запуск через «cmd /c start» со скрытым окном консоли
// (иначе при клике в меню на мгновение мелькает чёрное окно).
package tray

import (
	"os/exec"
	"syscall"
)

// openFile открывает файл приложением, сопоставленным расширению.
func openFile(path string) error {
	// Пустые кавычки — обязательный заголовок окна для start,
	// иначе путь в кавычках принимается за заголовок.
	cmd := exec.Command("cmd", "/c", "start", "", path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}
