package main

import (
	"log"
	"net"
	"os"

	"github.com/cloudwego/kitex/server"
	query_service "query-service/kitex_gen/stablepay/query_service/queryservice"
)

func main() {
	InitDB()

	// 启动 HTTP 内部接口（供 api-gateway 调用，默认 :8184）
	httpAddr := ":8184"
	if v := os.Getenv("HTTP_ADAPTER_ADDR"); v != "" {
		httpAddr = v
	}
	go startHTTPServer(httpAddr)

	// 监听 8084 端口
	addr, _ := net.ResolveTCPAddr("tcp", "0.0.0.0:8084")
	svr := query_service.NewServer(new(QueryServiceImpl), server.WithServiceAddr(addr))

	err := svr.Run()

	if err != nil {
		log.Println(err.Error())
	}
}
