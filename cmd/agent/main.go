// Точка входа агента homeping.
// Логика модуля: разбор флагов командной строки (-config, -test, -version,
// -no-tray), настройка логирования (файл с ротацией + stderr), запуск
// супервизора internal/agent и graceful shutdown по сигналам ОС.
// По умолчанию агент живёт в трее: systray занимает главную горутину
// (требование macOS), супервизор работает в фоне, ошибки конфига/токена
// не фатальны — агент ждёт исправления настроек. Флаг -no-tray включает
// headless-режим v1 с кодами выхода 2/3 (docs/spec.md, разделы 2 и 12).
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
	"homeping/internal/tray"
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
	noTray := flag.Bool("no-tray", false, "headless-режим: без иконки в трее и веб-интерфейса")
	flag.Parse()

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
	// живёт внутри супервизора (hot-reload). В headless-режиме ошибка
	// фатальна (код 2), в трей-режиме агент запускается и ждёт исправления.
	cfg, cfgErr := config.Load(*configPath)
	if cfgErr != nil && *noTray {
		fmt.Fprintf(os.Stderr, "ошибка конфигурации: %v\n", cfgErr)
		return 2
	}
	setupLogging(cfg)
	if cfgErr != nil {
		slog.Warn("конфигурация не загружена, агент ждёт настройки", "error", cfgErr)
	}

	// Контекст отменяется по Ctrl+C (SIGINT), SIGTERM или из меню трея —
	// все подсистемы агента обязаны завершаться по его отмене.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a := &agent.Agent{
		ConfigPath: *configPath,
		Notifier:   notify.Beeep{},
		FailFast:   *noTray,
		// При hot-reload применяется новый уровень логирования.
		OnConfig: func(c *config.Config) { logging.SetLevel(c.SlogLevel()) },
	}

	slog.Info("агент запускается",
		"version", version, "config", *configPath, "tray", !*noTray)

	if *noTray {
		return exitCode(a.Run(ctx))
	}

	// Трей-режим: systray блокирует главную горутину, агент — в фоне.
	// Остановка с любой стороны сходится в одну точку: отмена ctx →
	// супервизор завершается → цикл трея закрывается.
	done := make(chan error, 1)
	go func() {
		done <- a.Run(ctx)
		tray.Quit()
	}()
	tray.Run(tray.Options{
		Agent:       a,
		Version:     version,
		ConfigPath:  *configPath,
		RequestExit: stop,
	})
	return exitCode(<-done)
}

// exitCode отображает ошибку супервизора в код выхода процесса.
// Фатальные ошибки логируются через slog: он пишет и в stderr,
// и в файл логов — единственный след при запуске без консоли.
func exitCode(err error) int {
	switch {
	case err == nil:
		return 0
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

// setupLogging настраивает глобальный slog: файл с ротацией + stderr
// (docs/spec.md, раздел 11). Вызывается и при невалидном конфиге (nil) —
// тогда уровень info и путь по умолчанию. Недоступность файла логов
// не фатальна — агент важнее журнала, остаётся вывод в stderr.
func setupLogging(cfg *config.Config) {
	level := slog.LevelInfo
	logPath := ""
	if cfg != nil {
		level = cfg.SlogLevel()
		logPath = cfg.Logging.File
	}
	if logPath == "" {
		p, err := logging.DefaultPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "путь логов по умолчанию недоступен, логи только в stderr: %v\n", err)
		}
		logPath = p
	}
	logging.Setup(level, logPath)
}
