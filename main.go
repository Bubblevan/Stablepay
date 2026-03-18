package main

import (
	"log"
	query_service "query-service/kitex_gen/stablepay/query_service/queryservice"
)

func main() {
	InitDB()
	
	svr := query_service.NewServer(new(QueryServiceImpl))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
