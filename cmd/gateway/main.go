package main

import (
	"log"

	"github.com/bogachenko/tokenio-gateway/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatalf("gateway error: %v", err)
	}
}
