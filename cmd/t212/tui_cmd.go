package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ko5tas/t212/internal/tui"
)

func runTUI() error {
	host := os.Getenv("T212_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("T212_PORT")
	if port == "" {
		port = "8080"
	}

	wsURL := fmt.Sprintf("ws://%s:%s/ws", host, port)
	return tui.Run(context.Background(), wsURL)
}
