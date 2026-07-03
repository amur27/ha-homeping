<!-- Этот документ — инструкция по установке агента на клиентские машины: размещение бинарника и конфига, переменная окружения с токеном, автозапуск (Task Scheduler на Windows, launchd на macOS), проверка. -->

# Установка агента на Windows 11 и macOS

Предполагается, что бинарники собраны по task-06 (`homeping.exe` для Windows, `homeping` для macOS/arm64).

## Windows 11

### 1. Разместить файлы

```
C:\Program Files\homeping\homeping.exe
%APPDATA%\homeping\config.yaml
```

Конфиг — по образцу из [spec.md](spec.md), раздел 3.1.

### 2. Токен в переменную окружения пользователя

PowerShell (от имени текущего пользователя):

```powershell
[Environment]::SetEnvironmentVariable("HA_TOKEN", "<токен notify-agent-windows>", "User")
```

### 3. Проверка вручную

Открыть новый терминал (чтобы подхватилась переменная):

```powershell
& "C:\Program Files\homeping\homeping.exe" -test   # должен появиться тост
& "C:\Program Files\homeping\homeping.exe"          # рабочий запуск, смотреть лог
```

Открыть/закрыть дверь — уведомление должно прийти в течение секунды.

### 4. Автозапуск через планировщик задач

```powershell
$action  = New-ScheduledTaskAction -Execute "C:\Program Files\homeping\homeping.exe"
$trigger = New-ScheduledTaskTrigger -AtLogOn
$settings = New-ScheduledTaskSettingsSet -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1) `
    -ExecutionTimeLimit (New-TimeSpan -Seconds 0) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName "homeping" -Action $action -Trigger $trigger -Settings $settings
Start-ScheduledTask -TaskName "homeping"
```

Задача перезапускает агента при падении и не имеет лимита времени выполнения.

## macOS (Apple Silicon)

### 1. Разместить файлы

```
/usr/local/bin/homeping
~/Library/Application Support/homeping/config.yaml
```

Снять карантин Gatekeeper с бинарника, скопированного по сети:

```bash
xattr -d com.apple.quarantine /usr/local/bin/homeping
chmod +x /usr/local/bin/homeping
```

### 2. Проверка вручную

```bash
HA_TOKEN="<токен notify-agent-macbook>" homeping -test
```

При первом уведомлении macOS может спросить разрешение: System Settings → Notifications → разрешить для терминала/агента.

### 3. Автозапуск через launchd

Создать `~/Library/LaunchAgents/local.homeping.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>local.homeping</string>
    <key>ProgramArguments</key>
    <array><string>/usr/local/bin/homeping</string></array>
    <key>EnvironmentVariables</key>
    <dict><key>HA_TOKEN</key><string>ВСТАВИТЬ_ТОКЕН</string></dict>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>StandardOutPath</key><string>/tmp/homeping.log</string>
    <key>StandardErrorPath</key><string>/tmp/homeping.err</string>
</dict>
</plist>
```

Файл содержит токен — ограничить права и загрузить:

```bash
chmod 600 ~/Library/LaunchAgents/local.homeping.plist
launchctl load ~/Library/LaunchAgents/local.homeping.plist
```

`KeepAlive` перезапускает агента при падении; после сна ноутбука соединение восстановит встроенный бэкофф агента.

## Диагностика

| Симптом | Что проверить |
|---|---|
| Нет уведомлений вообще | `-test`; разрешения уведомлений в ОС; лог агента |
| «auth_invalid» в логе (выход с кодом 3) | Токен: не истёк ли, тот ли скопирован, видна ли переменная HA_TOKEN процессу |
| «connection refused» | Адрес/порт HA, доступность с этой машины: `curl http://<адрес>:8123/api/` |
| Уведомления приходят с задержкой после сна | Нормально в первые секунды после пробуждения (бэкофф); если дольше минуты — смотреть лог |
