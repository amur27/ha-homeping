// Тесты распознавания ошибки-фолбэка go-toast.
// Логика модуля: проверка shownViaFallback — ошибка нативного пути
// при успешном PowerShell-фолбэке подавляется, реальные неудачи
// (фолбэк тоже упал, иная ошибка) — нет.
package notify

import (
	"errors"
	"testing"
)

// TestShownViaFallback перебирает варианты ошибок beeep/go-toast.
func TestShownViaFallback(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "нативный путь упал на эмодзи, фолбэк показал (ошибки фолбэка нет)",
			err:  errors.New("doc.LoadXml(tmpl): error 3222070623 (FormatMessage failed ...)"),
			want: true,
		},
		{
			name: "фолбэк тоже упал — неудача настоящая",
			err: errors.Join(
				errors.New("doc.LoadXml(tmpl): error 3222070623"),
				errors.New(`executing powershell: "": exit status 1`),
			),
			want: false,
		},
		{
			name: "иная ошибка показа",
			err:  errors.New("The notification platform is unavailable"),
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shownViaFallback(tc.err); got != tc.want {
				t.Errorf("shownViaFallback(%v) = %v, ожидалось %v", tc.err, got, tc.want)
			}
		})
	}
}
