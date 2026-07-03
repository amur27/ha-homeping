#!/usr/bin/env bash
# Скрипт сборки релизных бинарников (запуск с macOS/Linux).
# Логика файла: кросс-компиляция агента под Windows (amd64) и macOS (arm64)
# без cgo, с зашивкой версии из git describe через ldflags и урезанием
# отладочной информации (-s -w). Результат — в каталоге dist/.

set -euo pipefail

# Работать из корня репозитория независимо от места запуска скрипта.
cd "$(dirname "$0")/.."

version="$(git describe --tags --always 2>/dev/null || echo unknown)"
ldflags="-s -w -X main.version=${version}"

echo "Сборка homecrier версии ${version}"

export CGO_ENABLED=0

GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "${ldflags}" \
    -o dist/homecrier.exe ./cmd/agent
echo "  dist/homecrier.exe (windows/amd64)"

GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "${ldflags}" \
    -o dist/homecrier-darwin-arm64 ./cmd/agent
echo "  dist/homecrier-darwin-arm64 (darwin/arm64)"

echo "Готово. Развёртывание — docs/setup-clients.md"
