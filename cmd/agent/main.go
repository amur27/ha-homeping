// Точка входа агента homeping.
// Логика модуля: разбор флагов командной строки (-config, -test, -version,
// -no-tray), настройка логирования (файл с ротацией + stderr, уровень меняется
// при hot-reload), запуск супервизора internal/agent и graceful shutdown
// по сигналам ОС. До появления трея (task-08) агент всегда работает
// в headless-режиме FailFast: коды выхода 2/3 как в v1 (docs/spec.md, раздел 12).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"homeping/internal/agent"
	"homeping/internal/config"
	"homeping/internal/hass"
	"homeping/internal/logging"
	"homeping/internal/notify"
)

// version зашивается при сборке релиза через ldflags (см. scripts/build.ps1).
var version = "dev"

func main() {
	os.Exit(run())
}

// run содержит всю логику процесса и возвращает код выхода
// (коды описаны в docs/spec.md, раздел 12). Выделена из main,
// чтобы defer-ы отрабатывали до os.Exit.
func run() int {
	configPath := flag.String("config", "", "путь к YAML-файлу конфигурации (по умолчанию — стандартный каталог настроек ОС)")
	testMode := flag.Bool("test", false, "показать пробное уведомление и выйти")
	showVersion := flag.Bool("version", false, "вывести версию и выйти")
	// Флаг заведён по спецификации v2; до реализации трея (task-08)
	// агент работает headless независимо от его значения.
	noTray := flag.Bool("no-tray", false, "headless-режим: без иконки в трее и веб-интерфейса")
	flag.Parse()
	_ = noTray

	if *showVersion {
		fmt.Println(version)
		return 0
	}

	// Если путь не задан флагом — используем стандартный для текущей ОС.
	if *configPath == "" {
		p, err := config.DefaultPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "не удалось определить путь конфигурации по умолчанию: %v\n", err)
			return 2
		}
		*configPath = p
	}

	if *testMode {
		// Пробное уведомление для проверки разрешений ОС;
		// конфиг и логирование для этого не нужны.
		if err := (notify.Beeep{}).Show("HomePing", "Агент работает — уведомления настроены правильно"); err != nil {
			fmt.Fprintf(os.Stderr, "не удалось показать пробное уведомление: %v\n", err)
			return 1
		}
		fmt.Println("пробное уведомление показано")
		return 0
	}

	// Первичная загрузка конфига — ради настроек логирования; дальше конфиг
	// живёт внутри супервизора (hot-reload). Ошибка — код 2 (headless-режим).
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка конфигурации: %v\n", err)
		return 2
	}
	setupLogging(cfg)

	// Контекст отменяется по Ctrl+C (SIGINT) или SIGTERM — все подсистемы
	// агента обязаны завершаться по его отмене.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("агент запускается",
		"version", version,
		"config", *configPath,
		"ha_url", cfg.HomeAssistant.URL,
		"entities", len(cfg.Entities))

	a := &agent.Agent{
		ConfigPath: *configPath,
		Notifier:   notify.Beeep{},
		// До task-08 трея нет — ошибки конфигурации и токена фатальны, как в v1.
		FailFast: true,
		// При hot-reload применяется новый уровень логирования.
		OnConfig: func(c *config.Config) { logging.SetLevel(c.SlogLevel()) },
	}

	// Фатальные ошибки логируются через slog: он пишет и в stderr,
	// и в файл логов — единственный след при запуске без консоли.
	if err := a.Run(ctx); err != nil {
		switch {
		case errors.Is(err, hass.ErrAuthInvalid):
			slog.Error("ошибка аутентификации", "error", err)
			return 3
		case errors.Is(err, agent.ErrConfig):
			slog.Error("агент не запущен", "error", err)
			return 2
		default:
			slog.Error("агент завершился с ошибкой", "error", err)
			return 1
		}
	}
	return 0
}

// setupLogging настраивает глобальный slog: файл с ротацией + stderr
// (docs/spec.md, раздел 11). Недоступность файла логов не фатальна —
// агент важнее журнала, остаётся вывод в stderr.
func setupLogging(cfg *config.Config) {
	logPath := cfg.Logging.File
	if logPath == "" {
		p, err := logging.DefaultPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "путь логов по умолчанию недоступен, логи только в stderr: %v\n", err)
		}
		logPath = p
	}
	logging.Setup(cfg.SlogLevel(), logPath)
}
