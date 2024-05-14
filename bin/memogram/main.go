package main

import (
	"context"

	"github.com/usememos/memogram"
)

func main() {
	ctx := context.Background()
	service, err := memogram.NewService()
	if err != nil {
		panic(err)
	}
	service.Start(ctx)
}
