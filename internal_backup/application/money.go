package application

import (
	"fmt"

	commonmoney "code.wenfu.cn/stablepayai/stablepay-common/money"
)

func normalizeBusinessAmount(req map[string]interface{}) error {
	amount := getString(req, "amount")
	currencyCode := getString(req, "currency")
	if amount == "" || currencyCode == "" {
		return nil
	}

	currency, ok := commonmoney.GetCurrencyByCode(currencyCode)
	if !ok {
		return fmt.Errorf("unsupported currency: %s", currencyCode)
	}

	moneyValue, err := commonmoney.NewMoneyFromDecimalString(amount, currency)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	req["amount"] = moneyValue.FormatDecimalAmount()
	req["amount_minor"] = moneyValue.GetString()
	req["currency"] = currency.Code
	return nil
}
