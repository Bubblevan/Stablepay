package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"

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
	var alipay *provider.Alipay
	if os.Getenv("ALIPAY_APP_ID") != "" && os.Getenv("ALIPAY_SELLER_ID") != "" &&
		os.Getenv("ALIPAY_PRIVATE_KEY_FILE") != "" && os.Getenv("ALIPAY_PUBLIC_KEY_FILE") != "" &&
		os.Getenv("ALIPAY_NOTIFY_URL") != "" && os.Getenv("ALIPAY_RETURN_URL") != "" {
		private, err := os.ReadFile(required("ALIPAY_PRIVATE_KEY_FILE"))
		if err != nil {
			log.Fatal("cannot read Alipay application private key")
		}
		public, err := os.ReadFile(required("ALIPAY_PUBLIC_KEY_FILE"))
		if err != nil {
			log.Fatal("cannot read Alipay public key")
		}
		alipay, err = provider.NewAlipay(provider.Config{AppID: required("ALIPAY_APP_ID"), SellerID: required("ALIPAY_SELLER_ID"), NotifyURL: required("ALIPAY_NOTIFY_URL"), ReturnURL: required("ALIPAY_RETURN_URL")}, private, public)
		if err != nil {
			log.Fatalf("configure Alipay adapter: %v", err)
		}
	}
	var machinePay *provider.MachinePay
	serviceID := os.Getenv("AIPAY_MACHINE_PAY_SERVICE_ID")
	machineAppID := strings.TrimSpace(os.Getenv("AIPAY_APP_ID"))
	if serviceID != "" || machineAppID != "" {
		if machineAppID == "" {
			machineAppID = strings.TrimSpace(os.Getenv("ALIPAY_APP_ID"))
		}
		if machineAppID == "" {
			log.Fatal("AIPAY_APP_ID or ALIPAY_APP_ID is required when Machine Pay is enabled")
		}
		machinePrivate, err := readMachinePaySecret("AIPAY_PRIVATE_KEY", "ALIPAY_PRIVATE_KEY", "AIPAY_PRIVATE_KEY_FILE", "ALIPAY_PRIVATE_KEY_FILE")
		if err != nil {
			log.Fatal("cannot read AI Pay application private key")
		}
		machinePublic, err := readMachinePaySecret("AIPAY_ALIPAY_PUBLIC_KEY", "ALIPAY_ALIPAY_PUBLIC_KEY", "AIPAY_ALIPAY_PUBLIC_KEY_FILE", "ALIPAY_PUBLIC_KEY_FILE")
		if err != nil {
			log.Fatal("cannot read AI Pay Alipay public key")
		}
		machinePay, err = provider.NewMachinePay(provider.MachinePayConfig{AppID: machineAppID, ServiceID: serviceID, GatewayURL: os.Getenv("AIPAY_GATEWAY_URL")}, machinePrivate, machinePublic)
		if err != nil {
			log.Fatalf("configure AI Pay Machine Pay adapter: %v", err)
		}
	}
	if alipay == nil && machinePay == nil {
		log.Fatal("configure at least one provider: ALIPAY_APP_ID or AIPAY_APP_ID with AIPAY_MACHINE_PAY_SERVICE_ID")
	}
	addr := &net.TCPAddr{IP: net.IPv4zero, Port: port}
	svr := service.NewServer(rpc.NewHandler(alipay, machinePay), server.WithServiceAddr(addr))
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

func readMachinePaySecret(primaryValue, fallbackValue, primaryFile, fallbackFile string) ([]byte, error) {
	if value := strings.TrimSpace(os.Getenv(primaryValue)); value != "" {
		return []byte(value), nil
	}
	if value := strings.TrimSpace(os.Getenv(fallbackValue)); value != "" {
		return []byte(value), nil
	}
	if path := strings.TrimSpace(os.Getenv(primaryFile)); path != "" {
		return os.ReadFile(path)
	}
	path := strings.TrimSpace(os.Getenv(fallbackFile))
	if path == "" {
		return nil, fmt.Errorf("%s or %s or a corresponding key file is required", primaryValue, fallbackValue)
	}
	return os.ReadFile(path)
}
