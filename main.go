package main

import (
	blockchain_adapter "github.com/stablepay/blockchain-adapter/kitex_gen/stablepay/blockchain_adapter/blockchainadapterservice"
	"log"
)

func main() {
	svr := blockchain_adapter.NewServer(new(BlockchainAdapterServiceImpl))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
