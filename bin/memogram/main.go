package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/usememos/memogram"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	service, err := memogram.NewService()
	if err != nil {
		panic(err)
	}
	service.Start(ctx)
}
