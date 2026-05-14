// Server entry point. Run with: go run main.go
// Note: generation.go also declares func main (key generation). Both files share
// package main intentionally — run them individually, never with go build ./...
package main

import (
	"gnark_server/server/api"
	"gnark_server/server/config"
)

func main() {
	cfg := config.Load()
	router := api.NewServer(cfg)
	if err := router.Run(":" + cfg.Port); err != nil {
		panic(err)
	}
}
