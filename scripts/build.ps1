# Скрипт сборки релизных бинарников (запуск с Windows).
# Логика файла: кросс-компиляция агента под Windows (amd64) и macOS (arm64)
# без cgo, с зашивкой версии из git describe через ldflags и урезанием
# отладочной информации (-s -w). Результат — в каталоге dist/.

$ErrorActionPreference = "Stop"

# Работать из корня репозитория независимо от места запуска скрипта.
Set-Location (Join-Path $PSScriptRoot "..")

$version = git describe --tags --always
if (-not $version) { $version = "unknown" }
$ldflags = "-s -w -X main.version=$version"

Write-Host "Сборка homeping версии $version"

$env:CGO_ENABLED = "0"

# Windows amd64
$env:GOOS = "windows"; $env:GOARCH = "amd64"
go build -trimpath -ldflags $ldflags -o dist/homeping.exe ./cmd/agent
if ($LASTEXITCODE -ne 0) { throw "сборка windows/amd64 не удалась" }
Write-Host "  dist/homeping.exe (windows/amd64)"

# macOS arm64 (Apple Silicon)
$env:GOOS = "darwin"; $env:GOARCH = "arm64"
go build -trimpath -ldflags $ldflags -o dist/homeping-darwin-arm64 ./cmd/agent
if ($LASTEXITCODE -ne 0) { throw "сборка darwin/arm64 не удалась" }
Write-Host "  dist/homeping-darwin-arm64 (darwin/arm64)"

# Сбросить переменные, чтобы не влиять на последующие команды в той же сессии.
Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host "Готово. Развёртывание — docs/setup-clients.md"
