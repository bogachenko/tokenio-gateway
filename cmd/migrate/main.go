package main

import (
	"context"
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

	os.Exit(app.MigrateMain(
		ctx,
		os.Getenv("TOKENIO_DATABASE_DSN"),
	))
}
