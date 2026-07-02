// Точка входа агента ha-notify-agent.
// Логика модуля: разбор флагов командной строки (-config, -test, -version),
// загрузка и валидация конфигурации, настройка логирования (slog),
// graceful shutdown по сигналам ОС и запуск основного цикла агента.
// Клиент Home Assistant и уведомления подключаются в task-03…05.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"ha-notify-agent/internal/config"
)

// version зашивается при сборке релиза через ldflags (см. task-06).
var version = "dev"

func main() {
	os.Exit(run())
}

// run содержит всю логику процесса и возвращает код выхода
// (коды описаны в docs/spec.md, раздел 8). Выделена из main,
// чтобы defer-ы отрабатывали до os.Exit.
func run() int {
	configPath := flag.String("config", "", "путь к YAML-файлу конфигурации (по умолчанию — стандартный каталог настроек ОС)")
	testMode := flag.Bool("test", false, "показать пробное уведомление и выйти")
	showVersion := flag.Bool("version", false, "вывести версию и выйти")
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

	// Загрузка и валидация конфигурации; любая ошибка — код выхода 2
	// (docs/spec.md, раздел 8).
	cfg, err := config.Load(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ошибка конфигурации: %v\n", err)
		return 2
	}
	setupLogging(cfg)

	if *testMode {
		// Пробное уведомление реализуется в task-04; конфиг при этом
		// уже проверен выше — режим -test валидирует и его.
		fmt.Println("режим -test будет реализован в task-04 (нативные уведомления)")
		return 0
	}

	// Токен нужен только для реальной работы с HA; в режимах -version/-test
	// он не требуется. Значение токена никогда не логируется.
	if _, err := cfg.Token(); err != nil {
		fmt.Fprintf(os.Stderr, "ошибка конфигурации: %v\n", err)
		return 2
	}

	// Контекст отменяется по Ctrl+C (SIGINT) или SIGTERM — все подсистемы
	// агента обязаны завершаться по его отмене.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("конфигурация загружена",
		"version", version,
		"config", *configPath,
		"ha_url", cfg.HomeAssistant.URL,
		"entities", len(cfg.Entities))

	// Основной цикл появится в task-03/05; каркас просто ждёт сигнала завершения.
	<-ctx.Done()

	slog.Info("получен сигнал завершения, агент останавливается")
	return 0
}

// setupLogging настраивает глобальный slog: текстовый вывод в stderr
// с уровнем из конфигурации (docs/spec.md, раздел 7). Файл журнала
// не открывается — перенаправление вывода обеспечивают Task Scheduler/launchd.
func setupLogging(cfg *config.Config) {
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	})
	slog.SetDefault(slog.New(handler))
}
