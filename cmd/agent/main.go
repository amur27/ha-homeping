// Точка входа агента ha-notify-agent.
// Логика модуля: разбор флагов командной строки (-config, -test, -version),
// определение пути к конфигурации, настройка graceful shutdown по сигналам ОС
// и запуск основного цикла агента. Сами подсистемы (конфиг, клиент Home Assistant,
// уведомления) подключаются в последующих задачах (task-02…05) — здесь каркас.
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

	if *testMode {
		// Пробное уведомление реализуется в task-04; пока каркас честно сообщает об этом.
		fmt.Println("режим -test будет реализован в task-04 (нативные уведомления)")
		return 0
	}

	// Контекст отменяется по Ctrl+C (SIGINT) или SIGTERM — все подсистемы
	// агента обязаны завершаться по его отмене.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("агент запущен (каркас)", "version", version, "config", *configPath)

	// Основной цикл появится в task-03/05; каркас просто ждёт сигнала завершения.
	<-ctx.Done()

	slog.Info("получен сигнал завершения, агент останавливается")
	return 0
}
