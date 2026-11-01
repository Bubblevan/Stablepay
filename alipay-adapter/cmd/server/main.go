package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"

	"github.com/cloudwego/kitex/server"
	"github.com/stablepay/alipay-adapter/internal/adapter/rpc"
	"github.com/stablepay/alipay-adapter/internal/provider"
	service "github.com/stablepay/alipay-adapter/kitex_gen/stablepay/alipay_adapter/alipayadapterservice"
)

func main() {
	port := 8086
	if raw := os.Getenv("ALIPAY_ADAPTER_RPC_PORT"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 65535 {
			log.Fatal("invalid ALIPAY_ADAPTER_RPC_PORT")
		}
		port = n
	}
	private, err := os.ReadFile(required("ALIPAY_PRIVATE_KEY_FILE"))
	if err != nil {
		log.Fatal("cannot read Alipay application private key")
	}
	public, err := os.ReadFile(required("ALIPAY_PUBLIC_KEY_FILE"))
	if err != nil {
		log.Fatal("cannot read Alipay public key")
	}
	alipay, err := provider.NewAlipay(provider.Config{AppID: required("ALIPAY_APP_ID"), SellerID: required("ALIPAY_SELLER_ID"), NotifyURL: required("ALIPAY_NOTIFY_URL"), ReturnURL: required("ALIPAY_RETURN_URL")}, private, public)
	if err != nil {
		log.Fatalf("configure Alipay adapter: %v", err)
	}
	addr := &net.TCPAddr{IP: net.IPv4zero, Port: port}
	svr := service.NewServer(rpc.NewHandler(alipay), server.WithServiceAddr(addr))
	log.Printf("Alipay Adapter Kitex RPC listening on %s", addr)
	if err := svr.Run(); err != nil {
		log.Fatal(fmt.Errorf("Alipay Adapter stopped: %w", err))
	}
}
func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("%s is required", name)
	}
	return value
}
