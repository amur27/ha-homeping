// Пакет logging отвечает за настройку журналирования агента.
// Логика модуля: основной приёмник логов — файл (Windows-бинарник собирается
// с -H=windowsgui, stdout недоступен) с простейшей ротацией — при достижении
// 5 МБ файл переименовывается в .old (одна копия) и пишется новый; вывод
// дублируется в stderr для запуска из терминала и режима -no-tray.
// Уровень задаётся через slog.LevelVar и меняется при hot-reload без
// пересоздания хендлера (docs/spec.md, раздел 11).
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// Параметры ротации (docs/spec.md, раздел 11).
const (
	maxFileSize = 5 << 20 // 5 МБ
	oldSuffix   = ".old"
)

// level — глобальный уровень логирования; меняется при hot-reload конфига.
var level slog.LevelVar

// DefaultPath возвращает путь к файлу логов по умолчанию для текущей ОС:
//   - Windows: %APPDATA%\homeping\homeping.log (рядом с конфигом)
//   - macOS:   ~/Library/Logs/homeping.log (стандартный каталог логов macOS)
func DefaultPath() (string, error) {
	if runtime.GOOS == "darwin" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("домашний каталог пользователя недоступен: %w", err)
		}
		return filepath.Join(home, "Library", "Logs", "homeping.log"), nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("каталог настроек пользователя недоступен: %w", err)
	}
	return filepath.Join(base, "homeping", "homeping.log"), nil
}

// Setup настраивает глобальный slog: текстовый вывод в файл с ротацией
// и в stderr. Пустой filePath допустим — тогда пишем только в stderr
// (файл недоступен — не повод не запускаться: уведомления важнее логов).
func Setup(lvl slog.Level, filePath string) {
	level.Set(lvl)

	var w io.Writer = os.Stderr
	if filePath != "" {
		// Не io.MultiWriter: он прекращает запись на первом же сбое,
		// а у GUI-процесса Windows (-H=windowsgui) stderr невалиден —
		// файл не получил бы ни строки. teeWriter пишет best-effort в оба.
		w = &teeWriter{
			file:   &rotatingWriter{path: filePath, maxSize: maxFileSize},
			stderr: os.Stderr,
		}
	}
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{Level: &level})
	slog.SetDefault(slog.New(handler))
}

// teeWriter дублирует записи в файл и stderr; сбой одного приёмника
// не мешает другому (без консоли живёт файл, без файла — консоль).
type teeWriter struct {
	file   io.Writer
	stderr io.Writer
}

// Write считается успешной, если запись принял хотя бы один приёмник.
func (t *teeWriter) Write(p []byte) (int, error) {
	_, errFile := t.file.Write(p)
	_, errStderr := t.stderr.Write(p)
	if errFile != nil && errStderr != nil {
		return 0, errFile
	}
	return len(p), nil
}

// SetLevel меняет уровень логирования на лету (hot-reload конфига).
func SetLevel(lvl slog.Level) {
	level.Set(lvl)
}

// rotatingWriter — io.Writer поверх файла с ротацией по размеру:
// при превышении maxSize текущий файл переименовывается в <path>.old
// (существующая копия затирается), запись продолжается в новый файл.
type rotatingWriter struct {
	path    string
	maxSize int64

	mu   sync.Mutex
	file *os.File
	size int64
}

// Write пишет порцию логов, при необходимости открывая и ротируя файл.
func (w *rotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// Close закрывает текущий файл логов (нужно тестам: на Windows открытый
// файл нельзя удалить). Следующая запись откроет файл заново.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}

// open открывает файл логов на дозапись, создавая каталог при необходимости,
// и запоминает текущий размер для контроля ротации.
func (w *rotatingWriter) open() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0o755); err != nil {
		return fmt.Errorf("каталог логов недоступен: %w", err)
	}
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("файл логов недоступен: %w", err)
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return fmt.Errorf("файл логов недоступен: %w", err)
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// rotate закрывает текущий файл, переносит его в .old и открывает новый.
// На Windows переименование поверх существующего файла невозможно,
// поэтому старая копия предварительно удаляется.
func (w *rotatingWriter) rotate() error {
	w.file.Close()
	w.file = nil
	if err := os.Remove(w.path + oldSuffix); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("не удалось удалить старую копию лога: %w", err)
	}
	if err := os.Rename(w.path, w.path+oldSuffix); err != nil {
		return fmt.Errorf("не удалось ротировать лог: %w", err)
	}
	return w.open()
}
