<!-- Этот файл — русская версия README: краткое описание решения, карта документации и порядок работы с ней. Основная (английская) версия — README.md. -->

[English](README.md) | **Русский**

# HomePing

**HomePing** — твой дом «пингует» тебя: доставка статусов датчиков Home Assistant (Raspberry Pi, Docker) в виде **нативных системных уведомлений** на ПК с Windows 11 и MacBook в локальной сети. Бинарник — `homeping`; историческое рабочее название проекта — HA-Notifications (имя каталога репозитория).

## Как это работает (в двух словах)

Лёгкий агент `homeping` (Go, один бинарник ~10 МБ) живёт в трее (Windows) / строке меню (macOS), подключается к WebSocket API Home Assistant по long-lived token, подписывается на события выбранных сущностей (например, `binary_sensor.front_door`) и при смене состояния показывает нативное уведомление ОС. Настраивается через встроенную страницу настроек в браузере (только `127.0.0.1`); токен хранится в системном хранилище учётных данных.

```
[Датчики] → [Home Assistant на RPi] ──WebSocket──> [homeping на Windows] → тост Windows
                                    ──WebSocket──> [homeping на macOS]   → уведомление macOS
```

## Карта документации

| Документ | Что внутри |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Архитектура: компоненты, потоки данных, безопасность, отказоустойчивость |
| [docs/adr/ADR-001-transport.md](docs/adr/ADR-001-transport.md) | Почему WebSocket API, а не MQTT / ntfy / Telegram |
| [docs/adr/ADR-002-language.md](docs/adr/ADR-002-language.md) | Почему агент на Go |
| [docs/adr/ADR-003-ui-stack.md](docs/adr/ADR-003-ui-stack.md) | UI v2: systray, локальный веб-интерфейс, keyring |
| [docs/spec.md](docs/spec.md) | Функциональная спецификация агента: конфиг, поведение, логирование |
| [docs/setup-home-assistant.md](docs/setup-home-assistant.md) | Подготовка Home Assistant: токен, проверка API |
| [docs/setup-clients.md](docs/setup-clients.md) | Установка и автозапуск агента на Windows и macOS |
| [docs/tasks/README.md](docs/tasks/README.md) | Декомпозиция реализации на задачи для ИИ-агентов |

## Сборка

Готовые дистрибутивы собирает GitHub Actions по тегу `v*` — см. страницу Releases. Локально (Go ≥ 1.22 и git):

```powershell
# Windows: dist/homeping.exe — иконка, GUI-подсистема (без консоли)
.\scripts\build.ps1
```

```bash
# macOS: dist/HomePing.app + zip (только на macOS — systray требует cgo)
./scripts/package-macos.sh
```

Версия зашивается из `git describe` и доступна через `homeping -version`, в меню трея и на странице настроек. Кросс-компиляция darwin-бинарника с Windows невозможна с v2 (cgo) — этим занимается CI.

Проверка после установки: «Тестовое уведомление» в меню трея (или `homeping -test`).

## Порядок работы

1. Прочитать [architecture.md](docs/architecture.md) и [spec.md](docs/spec.md).
2. Настроить Home Assistant по [setup-home-assistant.md](docs/setup-home-assistant.md) (токены, entity_id).
3. Собрать бинарники (раздел «Сборка») и развернуть по [setup-clients.md](docs/setup-clients.md).

## Статус

- **v1** (task-01…06) выполнена: ядро агента работает в эксплуатации.
- **v2** (task-07…10) реализована: трей, веб-интерфейс настроек, токен в keyring, hot-reload, упаковка и CI. Осталось: сквозной сценарий приёмки v2 на реальном окружении, включая MacBook — см. [docs/tasks/README.md](docs/tasks/README.md).
