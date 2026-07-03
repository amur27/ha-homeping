// Юнит-тесты пакета logging.
// Логика модуля: проверка ротации файла логов по размеру (создание .old,
// продолжение записи в новый файл) и дозаписи в существующий файл.
package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRotation: при превышении лимита файл уходит в .old, запись продолжается.
func TestRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	w := &rotatingWriter{path: path, maxSize: 100}
	defer w.Close()

	// Заполняем файл почти до лимита.
	if _, err := w.Write([]byte(strings.Repeat("a", 90))); err != nil {
		t.Fatalf("первая запись: %v", err)
	}
	// Эта запись превышает лимит — должна случиться ротация.
	if _, err := w.Write([]byte(strings.Repeat("b", 20))); err != nil {
		t.Fatalf("запись с ротацией: %v", err)
	}

	old, err := os.ReadFile(path + oldSuffix)
	if err != nil {
		t.Fatalf(".old после ротации не читается: %v", err)
	}
	if len(old) != 90 {
		t.Fatalf("в .old %d байт, ожидалось 90", len(old))
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("новый файл после ротации не читается: %v", err)
	}
	if string(cur) != strings.Repeat("b", 20) {
		t.Fatalf("новый файл содержит %q, ожидалась свежая запись", cur)
	}

	// Повторная ротация затирает предыдущий .old, а не падает.
	if _, err := w.Write([]byte(strings.Repeat("c", 100))); err != nil {
		t.Fatalf("вторая ротация: %v", err)
	}
	old, _ = os.ReadFile(path + oldSuffix)
	if string(old) != strings.Repeat("b", 20) {
		t.Fatalf(".old после второй ротации содержит %q, ожидалась предыдущая порция", old)
	}
}

// TestAppendExisting: writer продолжает существующий файл, а не затирает его.
func TestAppendExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")
	if err := os.WriteFile(path, []byte("старое\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	w := &rotatingWriter{path: path, maxSize: 1000}
	defer w.Close()
	if _, err := w.Write([]byte("новое\n")); err != nil {
		t.Fatalf("запись: %v", err)
	}

	data, _ := os.ReadFile(path)
	if string(data) != "старое\nновое\n" {
		t.Fatalf("файл содержит %q, дозапись не сработала", data)
	}
}
