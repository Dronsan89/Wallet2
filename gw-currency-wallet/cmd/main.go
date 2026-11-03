// Command wallet запускает сервис gw-currency-wallet.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/app"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/config"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/internal/logger"
	"github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg"
	pkgapp "github.com/Dronsan89/Wallet2/gw-currency-wallet/pkg/app"
)

// serviceName попадает в каждую запись лога.
const serviceName = "gw-currency-wallet"

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