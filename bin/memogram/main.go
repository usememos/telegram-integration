package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/usememos/memogram"
	"github.com/usememos/memogram/internal/version"
)

func main() {
	showVersion := flag.Bool("version", false, "Print the Memogram version and commit")
	flag.Parse()
	if err := version.Validate(); err != nil {
		slog.Error("invalid build metadata", slog.Any("err", err))
		os.Exit(1)
	}
	if *showVersion {
		fmt.Printf("memogram %s (commit %s)\n", version.Version, version.Commit)
		return
	}
	slog.Info("Memogram", slog.String("version", version.Version), slog.String("commit", version.Commit))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := memogram.NewService()
	if err != nil {
		panic(err)
	}
	service.Start(ctx)
}
