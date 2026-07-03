#!/usr/bin/env bash
# Скрипт сборки релизных бинарников (запуск с macOS).
# Логика файла: сборка darwin-бинарника (arm64, cgo — требование трея
# fyne.io/systray) и кросс-компиляция Windows-бинарника (amd64, без cgo,
# GUI-подсистема) с зашивкой версии из git describe и урезанием отладочной
# информации (-s -w). Результат — в каталоге dist/. Упаковка darwin-бинарника
# в .app-бандл — task-10 (scripts/package-macos.sh).

set -euo pipefail

# Работать из корня репозитория независимо от места запуска скрипта.
cd "$(dirname "$0")/.."

version="$(git describe --tags --always 2>/dev/null || echo unknown)"
ldflags="-s -w -X main.version=${version}"

echo "Сборка homeping версии ${version}"

# macOS arm64 (Apple Silicon): cgo обязателен для systray,
# поэтому darwin собирается только на macOS.
if [[ "$(uname -s)" == "Darwin" ]]; then
    CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "${ldflags}" \
        -o dist/homeping-darwin-arm64 ./cmd/agent
    echo "  dist/homeping-darwin-arm64 (darwin/arm64)"
else
    echo "  darwin/arm64 пропущен: сборка возможна только на macOS (cgo для systray)"
fi

# Windows amd64: кросс-компиляция без cgo, GUI-подсистема (без консоли).
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath \
    -ldflags "${ldflags} -H=windowsgui" \
    -o dist/homeping.exe ./cmd/agent
echo "  dist/homeping.exe (windows/amd64, GUI-подсистема)"

echo "Готово. Развёртывание — docs/setup-clients.md"
