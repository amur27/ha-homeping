// Открытие файла приложением по умолчанию — macOS.
// Логика модуля: штатная утилита open передаёт файл приложению,
// сопоставленному типу файла в Launch Services.
package tray

import "os/exec"

// openFile открывает файл приложением по умолчанию.
func openFile(path string) error {
	return exec.Command("open", path).Start()
}
