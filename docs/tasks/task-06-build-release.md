<!-- Этот документ — задача 06: сборка релиза — кросс-компиляция под Windows и macOS, зашивка версии, скрипты и чек-лист выпуска. -->

# Task 06 — Сборка и релиз

## Цель

Автоматизировать сборку релизных бинарников для Windows (amd64) и macOS (arm64) с зашитой версией; подготовить всё для развёртывания по [../setup-clients.md](../setup-clients.md).

## Контекст

- Спецификация: [../spec.md](../spec.md), разделы 2 (`-version`) и 9.
- Зависимость: task-05 выполнена, сквозной сценарий работает при ручном запуске.

## Шаги

1. Скрипт `scripts/build.ps1` (PowerShell, основная машина разработки — Windows):
   ```powershell
   # Сборка релизных бинарников для обеих платформ с зашивкой версии
   $version = git describe --tags --always
   $ldflags = "-s -w -X main.version=$version"
   $env:CGO_ENABLED = "0"
   $env:GOOS = "windows"; $env:GOARCH = "amd64"
   go build -ldflags $ldflags -o dist/homeping.exe ./cmd/agent
   $env:GOOS = "darwin"; $env:GOARCH = "arm64"
   go build -ldflags $ldflags -o dist/homeping-darwin-arm64 ./cmd/agent
   ```
   Плюс аналог `scripts/build.sh` для сборки с macOS/Linux.
2. Убедиться, что beeep собирается с `CGO_ENABLED=0` под обе платформы; если для macOS потребуется cgo — зафиксировать это в скрипте и в README отдельным примечанием (сборка darwin-бинарника тогда выполняется на MacBook).
3. Проверить `-version`: печатает версию из git-тега.
4. Дополнить README корня разделом «Сборка».
5. Прогнать полный сквозной сценарий приёмки из [README.md](README.md) пакета задач.

## Критерии приёмки

- [ ] `scripts/build.ps1` создаёт оба бинарника в `dist/` без ошибок.
- [ ] `homeping.exe -version` печатает версию, совпадающую с git describe.
- [ ] Windows-бинарник проходит `-test` и полный сценарий на ПК.
- [ ] macOS-бинарник запускается на MacBook (после `xattr -d com.apple.quarantine`) и проходит `-test`.
- [ ] Сквозной сценарий приёмки из оглавления задач пройден полностью на обеих машинах.
