package main

import (
	payment_service "github.com/stablepay/payment-service/kitex_gen/stablepay/payment_service/paymentservice"
	"log"
)

func main() {
	// 创建 PaymentServiceImpl 实例（包含依赖注入）
	impl := NewPaymentServiceImpl()

	// 创建 Kitex 服务器
	svr := payment_service.NewServer(impl)

	// 运行服务
	err := svr.Run()
	if err != nil {
		log.Println(err.Error())
	}
}
