package main

import (
	"os"

	"github.com/bogachenko/tokenio-gateway/internal/app"
)

func main() {
	os.Exit(app.GatewayMain())
}
