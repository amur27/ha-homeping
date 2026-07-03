//go:build !windows && !darwin

// Открытие файла приложением по умолчанию — прочие ОС (Linux и др.).
// Логика модуля: xdg-open из freedesktop; целевые ОС проекта — Windows
// и macOS, этот вариант нужен лишь для сборки на других платформах.
package tray

import "os/exec"

// openFile открывает файл приложением по умолчанию.
func openFile(path string) error {
	return exec.Command("xdg-open", path).Start()
}
