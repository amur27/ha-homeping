<!-- Этот документ — инструкция по установке агента v2 на клиентские машины: размещение дистрибутива, первый запуск с настройкой через веб-интерфейс, автозапуск (Task Scheduler на Windows, Login Items на macOS), логи и диагностика. -->

# Установка агента на Windows 11 и macOS (v2)

Дистрибутивы собирает GitHub Actions по тегу `v*` (см. `.github/workflows/release.yml`) — их можно скачать со страницы Releases репозитория. Локальная сборка: Windows — `scripts/build.ps1`, macOS — `scripts/package-macos.sh`.

С v2 агент живёт в трее и настраивается через встроенную страницу настроек — править YAML и задавать переменные окружения вручную больше не нужно.

## Windows 11

### 1. Разместить бинарник

```
C:\Program Files\homeping\homeping.exe
```

### 2. Первый запуск

Запустить `homeping.exe` (двойным кликом). Дальше всё само:

- в трее появится иконка-домик со статусом «не настроен»;
- будет создан конфиг-заготовка `%APPDATA%\homeping\config.yaml`;
- в браузере откроется страница настроек: ввести URL Home Assistant
  (`ws://<адрес>:8123/api/websocket`), вставить long-lived токен
  (профиль HA → Безопасность → Long-lived access tokens), настроить датчики.

Токен сохраняется в **диспетчере учётных данных Windows** (Credential Manager), не в файлах. Изменения применяются сразу, без перезапуска.

Проверка: кнопка «Тестовое уведомление» на странице настроек или в меню трея.

Обновление с v1: переменная окружения `HA_TOKEN` продолжает работать как резервный источник, но лучше один раз ввести токен на странице настроек и удалить переменную:

```powershell
[Environment]::SetEnvironmentVariable("HA_TOKEN", $null, "User")
```

### 3. Автозапуск через планировщик задач

```powershell
$action  = New-ScheduledTaskAction -Execute "C:\Program Files\homeping\homeping.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "homeping" -Action $action -Trigger $trigger -Settings $settings
Start-ScheduledTask -TaskName "homeping"
```

Перенаправление вывода не требуется: exe собран без консоли, логи агент пишет сам (см. «Логи»).

## macOS (Apple Silicon)

### 1. Разместить приложение

Распаковать `homeping-macos-arm64.zip` и перенести `HomePing.app` в `/Applications`. Дистрибутив не подписан — снять карантин Gatekeeper:

```bash
xattr -dr com.apple.quarantine /Applications/HomePing.app
```

### 2. Первый запуск

Запустить HomePing из Applications. Иконка появится в **строке меню** (в доке приложения нет — это фоновый агент), браузер откроет страницу настроек: URL, токен, датчики. Токен сохраняется в **Keychain**.

При первом уведомлении macOS спросит разрешение: System Settings → Notifications → разрешить для HomePing.

### 3. Автозапуск через Login Items

System Settings → General → Login Items → «+» → выбрать `/Applications/HomePing.app`.

(launchd-plist из v1 больше не нужен; если он остался — выгрузить: `launchctl unload ~/Library/LaunchAgents/local.homeping.plist` и удалить файл.)

## Меню трея

| Пункт | Что делает |
|---|---|
| Настройки… | страница настроек в браузере (доступна только с этого компьютера) |
| Тестовое уведомление | проверка разрешений ОС |
| Пауза уведомлений | события не показываются, соединение сохраняется |
| Открыть конфиг | YAML в редакторе по умолчанию (для ручной правки) |
| Перечитать конфиг | применить ручные правки YAML без перезапуска |
| Выход | остановить агента |

## Логи

- Windows: `%APPDATA%\homeping\homeping.log`
- macOS: `~/Library/Logs/homeping.log`

Ротация автоматическая: при 5 МБ файл переименовывается в `.old`.

## Диагностика

| Симптом | Что проверить |
|---|---|
| Нет уведомлений вообще | «Тестовое уведомление»; разрешения уведомлений в ОС; не включена ли «Пауза»; лог |
| Иконка с «!», статус «ошибка токена» | Токен отозван или истёк — вставить новый на странице настроек |
| Иконка с «!», статус «не настроен» | Токен ещё не задан — ввести на странице настроек |
| Иконка серая, статус «нет связи» | Адрес/порт HA, доступность с этой машины: `curl http://<адрес>:8123/api/`; агент переподключится сам |
| Страница настроек не открывается | Перезапустить агента; лог — строка «веб-интерфейс настроек запущен» |
| Уведомления приходят с задержкой после сна | Нормально в первые секунды после пробуждения (бэкофф); если дольше минуты — смотреть лог |
