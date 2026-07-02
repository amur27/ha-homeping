<!-- Этот файл — точка входа в проект: краткое описание решения, карта документации и порядок работы с ней. -->

# HA-Notifications

Доставка статусов датчиков Home Assistant (Raspberry Pi, Docker) в виде **нативных системных уведомлений** на ПК с Windows 11 и MacBook в локальной сети.

## Как это работает (в двух словах)

Лёгкий агент `ha-notify-agent` (Go, один бинарник) запускается на каждом компьютере, подключается к WebSocket API Home Assistant по long-lived token, подписывается на события выбранных сущностей (например, `binary_sensor.front_door`) и при смене состояния показывает нативное уведомление ОС.

```
[Датчики] → [Home Assistant на RPi] ──WebSocket──> [ha-notify-agent на Windows] → тост Windows
                                    ──WebSocket──> [ha-notify-agent на macOS]   → уведомление macOS
```

## Карта документации

| Документ | Что внутри |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Архитектура: компоненты, потоки данных, безопасность, отказоустойчивость |
| [docs/adr/ADR-001-transport.md](docs/adr/ADR-001-transport.md) | Почему WebSocket API, а не MQTT / ntfy / Telegram |
| [docs/adr/ADR-002-language.md](docs/adr/ADR-002-language.md) | Почему агент на Go |
| [docs/spec.md](docs/spec.md) | Функциональная спецификация агента: конфиг, поведение, логирование |
| [docs/setup-home-assistant.md](docs/setup-home-assistant.md) | Подготовка Home Assistant: токен, проверка API |
| [docs/setup-clients.md](docs/setup-clients.md) | Установка и автозапуск агента на Windows и macOS |
| [docs/tasks/README.md](docs/tasks/README.md) | Декомпозиция реализации на задачи для ИИ-агентов |

## Порядок работы

1. Прочитать [architecture.md](docs/architecture.md) и [spec.md](docs/spec.md).
2. Выполнить задачи из [docs/tasks/](docs/tasks/README.md) по порядку — каждая самодостаточна и имеет критерии приёмки.
3. Настроить окружения по инструкциям `setup-*.md`.

## Статус

Пакет документации готов; реализация агента — по задачам из `docs/tasks/`.
