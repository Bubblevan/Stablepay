package main

import (
	did_service "github.com/stablepay/did-service/kitex_gen/stablepay/did_service/didservice"
	"log"
)

func main() {
	svr := did_service.NewServer(new(DIDServiceImpl))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
