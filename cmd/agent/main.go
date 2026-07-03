// Точка входа агента homecrier.
// Логика модуля: разбор флагов командной строки (-config, -test, -version),
// загрузка и валидация конфигурации, настройка логирования (slog),
// graceful shutdown по сигналам ОС и запуск основного цикла агента.
// Клиент Home Assistant и уведомления подключаются в task-03…05.
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
	"time"

	"homecrier/internal/config"
	"homecrier/internal/hass"
	"homecrier/internal/notify"
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
		// Пробное уведомление для проверки разрешений ОС; конфиг
		// уже проверен выше — режим -test валидирует и его.
		if err := (notify.Beeep{}).Show("HomeCrier", "Агент работает — уведомления настроены правильно"); err != nil {
			fmt.Fprintf(os.Stderr, "не удалось показать пробное уведомление: %v\n", err)
			return 1
		}
		fmt.Println("пробное уведомление показано")
		return 0
	}

	// Токен нужен только для реальной работы с HA; в режимах -version/-test
	// он не требуется. Значение токена никогда не логируется.
	token, err := cfg.Token()
	if err != nil {
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

	// Список entity_id для подписки — из конфигурации.
	entityIDs := make([]string, len(cfg.Entities))
	for i, e := range cfg.Entities {
		entityIDs[i] = e.ID
	}
	client := hass.New(cfg.HomeAssistant.URL, token, entityIDs)

	// Потребитель событий: маршрутизатор строит тексты по конфигу,
	// применяет троттлинг и показывает нативные уведомления.
	router := notify.NewRouter(cfg.Entities, notify.Beeep{},
		time.Duration(cfg.Notifications.MinIntervalSec)*time.Second)
	go func() {
		for ev := range client.Events() {
			slog.Debug("событие", "entity", ev.EntityID, "state", ev.State)
			router.Handle(ev.EntityID, ev.State)
		}
	}()

	// Супервизор: переподключение с бэкоффом и одноразовые уведомления
	// о простое дольше 30 секунд / о восстановлении связи
	// (docs/spec.md, разделы 5 и 6).
	sup := &hass.Supervisor{
		Run: func(ctx context.Context, onReady func()) error {
			client.OnReady = onReady
			return client.Run(ctx)
		},
		Backoff:   hass.NewBackoff(),
		DownAfter: 30 * time.Second,
	}
	if cfg.NotifyOnDisconnect() {
		notifier := notify.Beeep{}
		sup.OnDown = func() {
			if err := notifier.Show("HomeCrier", "⚠️ Home Assistant недоступен"); err != nil {
				slog.Warn("не удалось показать уведомление о простое", "error", err)
			}
		}
		sup.OnUp = func() {
			if err := notifier.Show("HomeCrier", "✅ Связь с Home Assistant восстановлена"); err != nil {
				slog.Warn("не удалось показать уведомление о восстановлении", "error", err)
			}
		}
	}

	if err := sup.Loop(ctx); err != nil {
		if errors.Is(err, hass.ErrAuthInvalid) {
			fmt.Fprintf(os.Stderr, "ошибка аутентификации: %v\n", err)
			return 3
		}
		slog.Error("агент завершился с ошибкой", "error", err)
		return 1
	}

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
