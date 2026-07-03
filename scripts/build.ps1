# Скрипт сборки релизного бинарника Windows (запуск с Windows).
# Логика файла: сборка агента под Windows (amd64) с зашивкой версии из
# git describe, урезанием отладочной информации (-s -w) и GUI-подсистемой
# (-H=windowsgui — без консольного окна: агент живёт в трее, логи в файле).
# Darwin-бинарник с v2 здесь НЕ собирается: трей (fyne.io/systray) требует
# cgo на macOS, кросс-компиляция с Windows невозможна — сборка на MacBook
# (scripts/build.sh) или в GitHub Actions (task-10).

$ErrorActionPreference = "Stop"

# Работать из корня репозитория независимо от места запуска скрипта.
Set-Location (Join-Path $PSScriptRoot "..")

$version = git describe --tags --always
if (-not $version) { $version = "unknown" }
# -H=windowsgui: GUI-подсистема — при запуске не открывается консоль.
# Следствие: вывод -version/-test из консоли не виден; для отладки
# использовать dev-сборку (go build ./cmd/agent) или файл логов.
$ldflags = "-s -w -H=windowsgui -X main.version=$version"

Write-Host "Сборка homeping версии $version"

# Windows amd64 (на Windows cgo для systray не нужен).
$env:CGO_ENABLED = "0"
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -trimpath -ldflags $ldflags -o dist/homeping.exe ./cmd/agent
if ($LASTEXITCODE -ne 0) { throw "сборка windows/amd64 не удалась" }
Write-Host "  dist/homeping.exe (windows/amd64, GUI-подсистема)"

# Сбросить переменные, чтобы не влиять на последующие команды в той же сессии.
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "Готово. macOS собирается на MacBook: scripts/build.sh (см. task-10)."
Write-Host "Развёртывание — docs/setup-clients.md"
