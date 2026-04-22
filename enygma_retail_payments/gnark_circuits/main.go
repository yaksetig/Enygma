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
