package procurementmvp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stablepay/commerce-runtime/internal/llm"
)

type campusFakeClient struct {
	raw []byte
	err error
}

func (c campusFakeClient) GenerateDecision(context.Context, llm.LLMDecisionRequest) (llm.LLMDecisionResponse, error) {
	return llm.LLMDecisionResponse{Provider: "test-fixture", ModelRef: "fixture-only", RawJSON: c.raw}, c.err
}
func extractionFixture() (ExtractionRequest, []byte) {
	req := ExtractionRequest{Requirements: "活动名称：分享会。预算100元。横幅1条，规格3m × 0.8m。", Quotations: "横幅300cm × 80cm，单价80元，可开票。", Demo: true}
	value := func(v, source, quote string) *sourceValue {
		return &sourceValue{Value: v, Source: source, Quote: quote}
	}
	wire := wireCampus{Activity: value("分享会", "requirements", "活动名称：分享会。"), Budget: value("100", "requirements", "预算100元。"), Questions: []string{}, Items: []wireCampusItem{{Name: value("横幅", "requirements", "横幅1条"), RequestedSpec: value("3m × 0.8m", "requirements", "规格3m × 0.8m。"), QuotedSpec: value("300cm × 80cm", "quotations", "横幅300cm × 80cm"), Quantity: value("1", "requirements", "横幅1条"), UnitPrice: value("80", "quotations", "单价80元"), InvoiceStatement: value("可开票", "quotations", "可开票。")}}}
	raw, _ := json.Marshal(wire)
	return req, raw
}
func TestCampusExtractionEvidenceAndPersistence(t *testing.T) {
	service, _, file := setup(t)
	req, raw := extractionFixture()
	service.Planner.Client = campusFakeClient{raw: raw}
	ex, err := service.ExtractCampus(context.Background(), "owner", req)
	if err != nil {
		t.Fatal(err)
	}
	if ex.Draft.Approved || ex.Draft.Items[0].InvoiceConfirmed || ex.Draft.BudgetProof.State != "provided" || len(ex.Fields) != 8 {
		t.Fatalf("model gained authority: %+v", ex)
	}
	if _, err := service.GetExtraction("another", ex.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("extraction leaked")
	}
	if _, err := service.ReviewCampus("owner", CampusReviewRequest{Input: ex.Draft, ExtractionID: ex.ID}); err == nil {
		t.Fatal("confirmation bypass")
	}
	edited := ex.Draft
	edited.Items = append(edited.Items[:0:0], ex.Draft.Items...)
	edited.Items[0].UnitPrice = "60"
	edited.Demo = false
	review, err := service.ReviewCampus("owner", CampusReviewRequest{Input: edited, ExtractionID: ex.ID, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	if !review.Input.Demo || len(review.Changes) != 1 || review.Changes[0] != "items[0].unit_price" {
		t.Fatalf("audit provenance lost: %+v", review)
	}
	store, err := OpenStore(file)
	if err != nil {
		t.Fatal(err)
	}
	restarted := &Service{Store: store}
	exported, err := restarted.ExportCampusReview("owner", review.ID)
	if err != nil || !strings.Contains(exported, req.Quotations) || !strings.Contains(exported, ex.ResponseHash) || !strings.Contains(exported, "items[0].unit_price") {
		t.Fatal("export or restart lost provenance", err)
	}
	if _, err := restarted.ExportCampusReview("another", review.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("report leaked")
	}
}
func TestCampusRejectsFabricatedOrPrivilegedModelOutput(t *testing.T) {
	req, raw := extractionFixture()
	for _, bad := range []string{strings.Replace(string(raw), "单价80元", "伪造原文", 1), strings.Replace(string(raw), "\"budget\":", "\"approved\":true,\"budget\":", 1), strings.Replace(string(raw), "\"80\"", "\"8\"", 1), string(raw) + " {}"} {
		if _, err := parseCampusExtraction(req, llm.LLMDecisionResponse{RawJSON: []byte(bad)}); err == nil {
			t.Fatalf("accepted fabricated or privileged output: %s", bad)
		}
	}
}
func TestCampusUnavailableDoesNotFakeAI(t *testing.T) {
	service, _, _ := setup(t)
	req, _ := extractionFixture()
	if _, err := service.ExtractCampus(context.Background(), "owner", req); !errors.Is(err, ErrExtractionUnavailable) {
		t.Fatal(err)
	}
	service.Planner.Client = campusFakeClient{err: errors.New("offline")}
	if _, err := service.ExtractCampus(context.Background(), "owner", req); !errors.Is(err, ErrExtractionUnavailable) {
		t.Fatal(err)
	}
}
func TestCampusEndpointsAreProfileAndOriginRestricted(t *testing.T) {
	service, _, _ := setup(t)
	_, raw := extractionFixture()
	service.Planner.Client = campusFakeClient{raw: raw}
	for _, tc := range []struct {
		profile bool
		origin  string
		status  int
	}{{false, "http://localhost:8093", 404}, {true, "https://evil.example", 403}, {true, "http://localhost:8093", 400}} {
		request := httptest.NewRequest(http.MethodPost, "/api/mvp/extractions", strings.NewReader(`{}`))
		request.Header.Set("X-Requested-With", "StablePay")
		request.Header.Set("Origin", tc.origin)
		recorder := httptest.NewRecorder()
		Handler(service, HTTPConfig{CampusReview: tc.profile, AllowedOrigins: []string{"http://localhost:8093"}}).ServeHTTP(recorder, request)
		if recorder.Code != tc.status {
			t.Fatalf("got %d want %d", recorder.Code, tc.status)
		}
	}
}
