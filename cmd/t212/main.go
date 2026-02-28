package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: t212 <serve|tui>")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe()
	case "tui":
		err = runTUI()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
