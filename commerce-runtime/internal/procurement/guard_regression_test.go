package procurement

import "testing"

func completeInput() Input {
	proof := Evidence{State: "human_verified", Reference: "synthetic regression fixture", ReviewedBy: "fixture reviewer", ReviewedAt: "2026-01-01T00:00:00Z"}
	return Input{Activity: "回归样例", Budget: "100", BudgetEvidence: "fixture", Approved: true, BudgetProof: proof, ApprovalProof: proof, Items: []Item{{Name: "横幅", RequestedSpec: "3m × 0.8m", QuotedSpec: "300cm × 80cm", Quantity: 1, UnitPrice: "80", QuoteSource: "fixture", InvoiceConfirmed: true, QuoteProof: proof, InvoiceProof: proof}}}
}
func hasCode(r Report, code string) bool {
	for _, f := range r.Findings {
		if f.Code == code {
			return true
		}
	}
	return false
}
func TestCampusGuardRegression(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*Input)
		code    string
		invalid bool
	}{
		{"normal and equivalent units", func(*Input) {}, "", false},
		{"over budget", func(in *Input) { in.Budget = "50" }, "OVER_BUDGET", false},
		{"missing budget", func(in *Input) { in.Budget = "" }, "BUDGET_MISSING", false},
		{"different specification", func(in *Input) { in.Items[0].QuotedSpec = "60cm × 160cm" }, "SPEC_MISMATCH", false},
		{"invalid quantity", func(in *Input) { in.Items[0].Quantity = 0 }, "", true},
		{"negative money", func(in *Input) { in.Items[0].UnitPrice = "-1" }, "", true},
		{"fraction precision", func(in *Input) { in.Items[0].UnitPrice = "0.001" }, "", true},
		{"quote missing", func(in *Input) { in.Items[0].QuoteSource = "" }, "QUOTE_SOURCE_MISSING", false},
		{"approval pending", func(in *Input) { in.Approved = false }, "APPROVAL_PENDING", false},
		{"invoice unknown", func(in *Input) { in.Items[0].InvoiceConfirmed = false }, "INVOICE_UNCONFIRMED", false},
		{"budget label does not verify", func(in *Input) { in.BudgetProof = Evidence{} }, "BUDGET_EVIDENCE_UNVERIFIED", false},
		{"provided quote does not verify", func(in *Input) { in.Items[0].QuoteProof = Evidence{State: "provided", Reference: "file.pdf"} }, "QUOTE_SOURCE_UNVERIFIED", false},
		{"incomplete manual attestation", func(in *Input) { in.ApprovalProof.ReviewedBy = "" }, "APPROVAL_UNVERIFIED", false},
		{"demo cannot be cleared by attestation", func(in *Input) { in.Demo = true }, "DEMO_DATA", false},
		{"missing specification", func(in *Input) { in.Items[0].RequestedSpec = "" }, "SPEC_MISSING", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := completeInput()
			tc.change(&in)
			r, err := Audit(in, false)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid facts accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.code == "" && len(r.Findings) != 0 {
				t.Fatalf("normal sample flagged: %+v", r.Findings)
			}
			if tc.code != "" && !hasCode(r, tc.code) {
				t.Fatalf("missing %s: %+v", tc.code, r.Findings)
			}
		})
	}
}
func TestLengthUnitsAndDimensionBoundaries(t *testing.T) {
	if NormalizeSpec("3m x 0.8m") != NormalizeSpec("300cm × 80cm") {
		t.Fatal("ASCII dimensions not converted")
	}
	if NormalizeSpec("XL") == NormalizeSpec("×L") {
		t.Fatal("ordinary x was treated as a dimension operator")
	}
	for _, pair := range [][2]string{{"3m × 0.8m", "300cm × 80cm"}, {"1米*20厘米", "1000mm × 200mm"}, {"0.1mm", "0.01cm"}, {"24 瓶 / 箱", "24瓶/箱"}} {
		if NormalizeSpec(pair[0]) != NormalizeSpec(pair[1]) {
			t.Errorf("not equivalent: %q %q => %s / %s", pair[0], pair[1], NormalizeSpec(pair[0]), NormalizeSpec(pair[1]))
		}
	}
	for _, pair := range [][2]string{{"3×08", "30×8"}, {"500ml", "500m"}, {"80cm × 180cm", "60cm × 160cm"}} {
		if NormalizeSpec(pair[0]) == NormalizeSpec(pair[1]) {
			t.Errorf("false equivalence %v", pair)
		}
	}
}

func TestSchoolPolicyRequiresExplicitScope(t *testing.T) {
	in := completeInput()
	in.PolicyID = "cup-club-fund-20260327"
	r, err := Audit(in, false)
	if err != nil || r.Policy == nil || r.Policy.Applied || !hasCode(r, "POLICY_SCOPE_UNCONFIRMED") {
		t.Fatal("policy was applied to an unknown school", err)
	}
	in.School = "中国石油大学（北京）"
	in.PolicyScopeConfirmed = true
	r, err = Audit(in, false)
	if err != nil || !r.Policy.Applied || r.Policy.Version != "官网发布版 2026-03-27" || !hasCode(r, "POLICY_MANUAL_REVIEW") {
		t.Fatal("policy lacks versioned manual checks", err)
	}
}
