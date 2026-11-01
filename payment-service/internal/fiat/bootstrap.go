package fiat

import "fmt"

func NewProvider(mode, adapterAddress string, timeoutMs, retryCount int) (Provider, error) {
	if mode == "" || mode == "local" {
		return LocalProvider{}, nil
	}
	if mode != "alipay_sandbox" {
		return nil, fmt.Errorf("unsupported FIAT_PAYMENT_MODE %q", mode)
	}
	return NewKitexProvider(adapterAddress, timeoutMs, retryCount)
}
