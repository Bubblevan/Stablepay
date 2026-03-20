package main

import (
	"log"
	"net"

	"github.com/cloudwego/kitex/server"
	query_service "query-service/kitex_gen/stablepay/query_service/queryservice"
)

func main() {
	InitDB()

	// 监听 8084 端口
	addr, _ := net.ResolveTCPAddr("tcp", "localhost:8084")
	svr := query_service.NewServer(new(QueryServiceImpl), server.WithServiceAddr(addr))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
