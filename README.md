<!-- Этот файл — основная (английская) точка входа в проект: краткое описание решения, карта документации и порядок работы. Русская версия — README.ru.md. -->

**English** | [Русский](README.ru.md)

# HomePing

[![Release](https://img.shields.io/github/v/release/amur27/ha-homeping)](https://github.com/amur27/ha-homeping/releases/latest)

**HomePing** — your home "pings" you: it delivers Home Assistant sensor state changes (Raspberry Pi, Docker) as **native desktop notifications** to Windows 11 and macOS machines on your local network. The binary is `homeping`; the historical working name of the project is HA-Notifications (the repository directory name).

## How it works (in a nutshell)

A lightweight `homeping` agent (Go, single ~10 MB binary) lives in the system tray (Windows) / menu bar (macOS), connects to the Home Assistant WebSocket API with a long-lived access token, subscribes to state changes of the entities you pick (e.g. `binary_sensor.front_door`), and shows a native OS notification whenever a state changes. It is configured through a built-in settings page in your browser (bound to `127.0.0.1` only); the token is kept in the OS credential store (Windows Credential Manager / macOS Keychain).

```
[Sensors] → [Home Assistant on RPi] ──WebSocket──> [homeping on Windows] → Windows toast
                                    ──WebSocket──> [homeping on macOS]   → macOS notification
```

Key properties:

- **Zero extra infrastructure** — talks to the stock Home Assistant WebSocket API; no MQTT broker, no cloud, no push services.
- **Instant and resilient** — server-side event filtering (`subscribe_trigger`), exponential-backoff reconnect, ping/pong liveness checks, one-shot "HA is unreachable / connection restored" notifications.
- **Lightweight** — single static binary, no runtimes, steady-state memory in the tens of megabytes.
- **Hot-reload** — settings saved from the web UI (or a manual YAML edit + "Reload config") apply without restarting the agent.

## Documentation map

The documentation is written in Russian (the project's working language).

| Document | What's inside |
|---|---|
| [docs/architecture.md](docs/architecture.md) | Architecture: components, data flows, security, resilience |
| [docs/adr/ADR-001-transport.md](docs/adr/ADR-001-transport.md) | Why the WebSocket API and not MQTT / ntfy / Telegram |
| [docs/adr/ADR-002-language.md](docs/adr/ADR-002-language.md) | Why the agent is written in Go |
| [docs/adr/ADR-003-ui-stack.md](docs/adr/ADR-003-ui-stack.md) | UI v2: systray, local web UI, keyring |
| [docs/spec.md](docs/spec.md) | Functional specification: config, behaviour, tray, web UI, logging |
| [docs/setup-home-assistant.md](docs/setup-home-assistant.md) | Home Assistant preparation: token, API check |
| [docs/setup-clients.md](docs/setup-clients.md) | Installing and autostarting the agent on Windows and macOS |
| [docs/tasks/README.md](docs/tasks/README.md) | Implementation breakdown into tasks for AI agents |

## Installation

Grab a build from the [Releases](https://github.com/amur27/ha-homeping/releases) page:

- `homeping.exe` — Windows 11 (amd64), GUI subsystem (no console window);
- `homeping-macos-arm64.zip` — `HomePing.app` for macOS (Apple Silicon), menu-bar only (`LSUIElement`).

Run it once — the agent creates a starter config and opens the settings page where you enter the HA URL and token. See [docs/setup-clients.md](docs/setup-clients.md) for autostart setup and troubleshooting.

## Building from source

Release builds are produced by GitHub Actions on `v*` tags. Locally (Go ≥ 1.22 and git):

```powershell
# Windows: dist/homeping.exe — icon, GUI subsystem (no console)
.\scripts\build.ps1
```

```bash
# macOS: dist/HomePing.app + zip (macOS only — systray requires cgo)
./scripts/package-macos.sh
```

The version is embedded from `git describe` and is visible via `homeping -version`, in the tray menu and on the settings page. Cross-compiling the darwin binary from Windows is not possible since v2 (cgo) — CI takes care of it.

Post-install check: "Test notification" in the tray menu (or `homeping -test`).

## Status

- **v1** (task-01…06) done: the agent core is in production use.
- **v2** (task-07…10) implemented: tray, web settings UI, token in the OS keyring, hot-reload, packaging and CI. Remaining: the end-to-end v2 acceptance run on real hardware, including a MacBook — see [docs/tasks/README.md](docs/tasks/README.md).
