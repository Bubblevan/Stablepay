package main

import (
	"log"

	"github.com/stablepay/did-service/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
