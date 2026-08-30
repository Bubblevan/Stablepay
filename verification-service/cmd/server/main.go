package main

import (
	"log"

	"github.com/stablepay/verification-service/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
