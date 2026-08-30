package main

import (
	verification_service "github.com/stablepay/verification-service/kitex_gen/stablepay/verification_service/verificationservice"
	"log"
)

func main() {
	svr := verification_service.NewServer(new(VerificationServiceImpl))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
