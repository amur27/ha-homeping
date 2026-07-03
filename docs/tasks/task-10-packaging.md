<!-- Этот документ — задача 10: упаковка и CI для v2 — windowsgui-сборка с иконкой, .app-бандл для macOS (LSUIElement), сборка darwin в GitHub Actions, обновление инструкций установки. -->

# Task 10 — Упаковка и CI

## Цель

Довести v2 до устанавливаемого вида: Windows-exe без консоли и с иконкой, macOS `.app`-бандл, живущий только в строке меню, автоматическая сборка darwin в CI (cgo не позволяет собирать его с Windows), обновлённые инструкции установки.

## Контекст

- Спецификация: [../spec.md](../spec.md), разделы 11–13; последствия в [../adr/ADR-003-ui-stack.md](../adr/ADR-003-ui-stack.md).
- Зависимости: task-08, task-09 — функциональность v2 полная.

## Шаги

1. Windows: иконка и метаданные exe через `github.com/tc-hib/go-winres` (или аналог) — генерируемый `.syso`; в `scripts/build.ps1` уже есть `-H=windowsgui` (task-08). Проверить автозапуск через Task Scheduler: перенаправление вывода больше не нужно (логи в файле).
2. macOS: скрипт `scripts/package-macos.sh` — сборка бинарника и сборка `homeping.app` (структура `Contents/MacOS`, `Contents/Resources/icon.icns`, `Info.plist` с `LSUIElement=true`, `CFBundleIdentifier`, версией из git). Автозапуск — через Login Items (`.app`), инструкция взамен launchd-plist.
3. GitHub Actions `.github/workflows/release.yml`: по тегу `v*` — job на `windows-latest` (exe) и `macos-latest` (`.app` в zip), артефакты прикладываются к GitHub Release. Без подписи кода: в инструкции — снятие карантина `xattr -d com.apple.quarantine`.
4. Обновить [../setup-clients.md](../setup-clients.md) и корневой README под v2: установка, первый запуск (страница настроек открывается сама), автозапуск на обеих ОС, расположение логов.
5. Прогнать сквозной сценарий приёмки v2 из [README.md](README.md) пакета задач.

## Критерии приёмки

- [ ] `scripts/build.ps1` собирает Windows-exe с иконкой и без консольного окна.
- [ ] `homeping.app` на MacBook: иконка только в строке меню (в доке нет), настройки и уведомления работают.
- [ ] Workflow по тегу собирает оба артефакта; darwin-сборка проходит с cgo на macos-раннере.
- [ ] `homeping.exe -version` и версия в `Info.plist` совпадают с git-тегом.
- [ ] `setup-clients.md` актуален: свежая установка по инструкции на обеих машинах проходит без обращения к другим документам.
- [ ] Сквозной сценарий приёмки v2 пройден полностью.
