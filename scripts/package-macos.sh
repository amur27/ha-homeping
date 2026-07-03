#!/usr/bin/env bash
# Скрипт упаковки macOS-дистрибутива (запуск только на macOS: cgo для systray).
# Логика файла: сборка darwin-бинарника (arm64) и сборка бандла
# dist/HomePing.app — Contents/MacOS/homeping, Info.plist с версией
# из git describe (__VERSION__ в шаблоне packaging/macos/Info.plist),
# иконка icon.icns; итог упаковывается в dist/homeping-macos-arm64.zip
# для выкладки в GitHub Release (task-10). Подписи кода нет — после
# распаковки на другой машине снять карантин: xattr -dr com.apple.quarantine.

set -euo pipefail

cd "$(dirname "$0")/.."

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo "ошибка: упаковка .app возможна только на macOS (cgo для systray)" >&2
    exit 1
fi

version="$(git describe --tags --always 2>/dev/null || echo unknown)"
ldflags="-s -w -X main.version=${version}"
app="dist/HomePing.app"

echo "Упаковка HomePing.app версии ${version}"

rm -rf "$app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"

CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "${ldflags}" \
    -o "$app/Contents/MacOS/homeping" ./cmd/agent

# Версия в Info.plist — без префикса v (соглашение CFBundleShortVersionString).
sed "s/__VERSION__/${version#v}/g" packaging/macos/Info.plist > "$app/Contents/Info.plist"
cp packaging/macos/icon.icns "$app/Contents/Resources/icon.icns"

(cd dist && rm -f homeping-macos-arm64.zip && zip -qry homeping-macos-arm64.zip HomePing.app)

echo "Готово:"
echo "  $app"
echo "  dist/homeping-macos-arm64.zip"
