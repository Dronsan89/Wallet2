// Command notification запускает сервис gw-notification.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Dronsan89/Wallet2/gw-notification/internal/app"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/config"
	"github.com/Dronsan89/Wallet2/gw-notification/internal/logger"
	"github.com/Dronsan89/Wallet2/gw-notification/pkg"
	pkgapp "github.com/Dronsan89/Wallet2/gw-notification/pkg/app"
)

// serviceName попадает в каждую запись лога.
const serviceName = "gw-notification"

func main() {
	configPath := flag.String("c", "config.env", "путь к файлу конфигурации")
	flag.Parse()

	if err := run(*configPath); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", serviceName, err)
		os.Exit(1)
	}
}

// run загружает конфигурацию, собирает приложение и ждёт сигнала остановки.
func run(configPath string) error {
	if err := config.LoadEnv(configPath); err != nil {
		return err
	}

	log := logger.New(pkg.GetEnv("LOG_LEVEL", "info"), serviceName)
	cfg := config.New()

	return pkgapp.Run(app.New(cfg, log), log)
}