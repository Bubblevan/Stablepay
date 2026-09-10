package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/cloudwego/kitex/client"
	common "github.com/stablepay/payment-service/kitex_gen/stablepay/common"
	payment "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service"
	paymentservice "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
)

func main() {
	address := flag.String("address", "127.0.0.1:8888", "payment-service Kitex address")
	txID := flag.String("tx-id", "", "payment transaction ID")
	flag.Parse()
	if *txID == "" {
		fail("tx-id is required")
	}
	cli, err := paymentservice.NewClient("payment-service", client.WithHostPorts(*address), client.WithRPCTimeout(10*time.Second))
	if err != nil {
		fail("create client: %v", err)
	}
	resp, err := cli.GetPaymentStatus(context.Background(), &payment.GetPaymentStatusRequest{
		Base: common.NewBaseReq(), TxId: common.TxId(*txID),
	})
	if err != nil {
		fail("query payment: %v", err)
	}
	if resp.GetBase() != nil && resp.GetBase().GetCode() != 0 {
		fail("query payment rejected: %s", resp.GetBase().GetMessage())
	}
	out := map[string]interface{}{
		"tx_id": resp.GetTxId(), "status": resp.GetStatus().String(),
		"tx_hash": resp.GetTxHash(), "confirmed_at": resp.GetConfirmedAt(), "failed_at": resp.GetFailedAt(),
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fail("encode response: %v", err)
	}
}

func fail(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
