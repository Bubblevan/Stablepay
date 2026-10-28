package procurement

import "testing"

func TestJointReviewEvidence(t *testing.T) {
	input := Input{Activity: "分享会", Budget: "500", Approved: true, BudgetEvidence: "演示批复", Items: []Item{{Name: "横幅", Quantity: 1, UnitPrice: "180", RequestedSpec: "3m", QuotedSpec: "3m", QuoteSource: "报价A", InvoiceConfirmed: true}, {Name: "展架", Quantity: 2, UnitPrice: "68", RequestedSpec: "80cm", QuotedSpec: "60cm", QuoteSource: "报价B", InvoiceConfirmed: true}, {Name: "水", Quantity: 4, UnitPrice: "35", RequestedSpec: "24瓶", QuotedSpec: "24瓶", QuoteSource: "报价C"}, {Name: "桌牌", Quantity: 4, UnitPrice: "20", RequestedSpec: "A5", QuotedSpec: "A5", QuoteSource: "报价D", InvoiceConfirmed: true}}}
	proof := Evidence{State: "human_verified", Reference: "synthetic fixture only", ReviewedBy: "test fixture", ReviewedAt: "2026-01-01T00:00:00Z"}
	input.BudgetProof, input.ApprovalProof = proof, proof
	for i := range input.Items {
		input.Items[i].QuoteProof, input.Items[i].InvoiceProof = proof, proof
	}
	report, err := Audit(input, false)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalMinor != 53600 || len(report.Findings) != 3 {
		t.Fatalf("unexpected report: %+v", report)
	}
	for i, expected := range []string{"OVER_BUDGET", "SPEC_MISMATCH", "INVOICE_UNCONFIRMED"} {
		if report.Findings[i].Code != expected || report.Findings[i].Evidence == "" || report.Findings[i].Action == "" {
			t.Fatalf("missing independently inspectable finding: %+v", report.Findings[i])
		}
	}
	budget, err := Audit(input, true)
	if err != nil || len(budget.Findings) != 1 {
		t.Fatal("budget-only service leaked full review", err)
	}
}
func TestInvalidMoneyAndQuantitiesAbstain(t *testing.T) {
	for _, raw := range []string{"0.001", "1e2", "-1", "NaN", "9999999999"} {
		if _, err := ParseMoney(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if value, err := ParseMoney("0.29"); value != 29 || err != nil {
		t.Fatal(value, err)
	}
	in := Input{Activity: "活动", Budget: "100", Items: []Item{{Name: "纸", Quantity: -1, UnitPrice: "1"}}}
	if _, err := Audit(in, false); err == nil {
		t.Fatal("invalid source data was silently priced")
	}
}
