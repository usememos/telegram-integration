package main

import "github.com/usememos/memogram"

func main() {
	service, err := memogram.NewService()
	if err != nil {
		panic(err)
	}
	service.Start()
}
