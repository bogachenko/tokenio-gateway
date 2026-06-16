package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/bogachenko/tokenio-gateway/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	if err := app.RunMigrations(
		ctx,
		os.Getenv("TOKENIO_DATABASE_DSN"),
	); err != nil {
		log.Fatalf("migration error: %v", err)
	}
}
