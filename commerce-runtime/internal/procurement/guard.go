// Package procurement is the deterministic guard shared by the campus and payment MVPs.
package procurement

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Evidence records a user assertion or manual review, never platform verification.
type Evidence struct {
	State      string `json:"state"`
	Reference  string `json:"reference"`
	ReviewedBy string `json:"reviewed_by"`
	ReviewedAt string `json:"reviewed_at"`
}

func (e Evidence) ManuallyReviewed() bool {
	at, err := time.Parse(time.RFC3339, e.ReviewedAt)
	return e.State == "human_verified" && strings.TrimSpace(e.Reference) != "" && strings.TrimSpace(e.ReviewedBy) != "" && err == nil && !at.After(time.Now().Add(time.Minute))
}
func (e Evidence) validate() bool {
	return (e.State == "" || e.State == "declared" || e.State == "provided" || e.State == "human_verified") && len(e.Reference) <= 1000 && len(e.ReviewedBy) <= 100 && len(e.ReviewedAt) <= 40
}

type Item struct {
	Name             string   `json:"name"`
	RequestedSpec    string   `json:"requested_spec"`
	QuotedSpec       string   `json:"quoted_spec"`
	Quantity         int64    `json:"quantity"`
	UnitPrice        string   `json:"unit_price"`
	QuoteSource      string   `json:"quote_source"`
	InvoiceConfirmed bool     `json:"invoice_confirmed"`
	QuoteProof       Evidence `json:"quote_proof"`
	InvoiceProof     Evidence `json:"invoice_proof"`
}
type Input struct {
	Activity             string   `json:"activity"`
	School               string   `json:"school"`
	Budget               string   `json:"budget"`
	BudgetEvidence       string   `json:"budget_evidence"`
	Approved             bool     `json:"approved"`
	Items                []Item   `json:"items"`
	BudgetProof          Evidence `json:"budget_proof"`
	ApprovalProof        Evidence `json:"approval_proof"`
	Demo                 bool     `json:"demo"`
	PolicyID             string   `json:"policy_id"`
	PolicyScopeConfirmed bool     `json:"policy_scope_confirmed"`
}
type Finding struct {
	Code      string `json:"code"`
	Severity  string `json:"severity"`
	Title     string `json:"title"`
	Evidence  string `json:"evidence"`
	Action    string `json:"action"`
	ItemIndex *int   `json:"item_index,omitempty"`
}
type Report struct {
	Activity     string           `json:"activity"`
	BudgetMinor  int64            `json:"budget_minor"`
	TotalMinor   int64            `json:"total_minor"`
	Findings     []Finding        `json:"findings"`
	Scope        string           `json:"scope"`
	RulesVersion string           `json:"rules_version"`
	Policy       *PolicyReference `json:"policy,omitempty"`
}

func ParseMoney(raw string) (int64, error) {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) > 2 || parts[0] == "" || len(parts[0]) > 9 {
		return 0, errors.New("金额必须为非负十进制，最多两位小数")
	}
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return 0, errors.New("金额包含非法字符")
		}
	}
	whole, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, err
	}
	fraction := "00"
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 2 {
			return 0, errors.New("金额最多两位小数")
		}
		fraction = parts[1] + "0"
	}
	if fraction[0] < '0' || fraction[0] > '9' || fraction[1] < '0' || fraction[1] > '9' {
		return 0, errors.New("金额包含非法字符")
	}
	return whole*100 + int64(fraction[0]-'0')*10 + int64(fraction[1]-'0'), nil
}
func FormatMoney(cents int64) string { return fmt.Sprintf("¥%d.%02d", cents/100, cents%100) }
func (in Input) Validate() error {
	if in.PolicyID != "" && in.PolicyID != "cup-club-fund-20260327" {
		return errors.New("不支持的校规参考包")
	}
	if strings.TrimSpace(in.Activity) == "" || len(in.Activity) > 200 || len(in.School) > 200 || len(in.BudgetEvidence) > 1000 || len(in.Items) == 0 || len(in.Items) > 50 || !in.BudgetProof.validate() || !in.ApprovalProof.validate() {
		return errors.New("请填写活动名称及 1–50 项采购明细")
	}
	if strings.TrimSpace(in.Budget) != "" {
		if _, err := ParseMoney(in.Budget); err != nil {
			return errors.New("请填写有效活动预算")
		}
	}
	for _, item := range in.Items {
		price, err := ParseMoney(item.UnitPrice)
		if err != nil || price <= 0 || item.Quantity <= 0 || item.Quantity > 100000 || strings.TrimSpace(item.Name) == "" || len(item.Name) > 200 || len(item.RequestedSpec) > 500 || len(item.QuotedSpec) > 500 || len(item.QuoteSource) > 1000 || !item.QuoteProof.validate() || !item.InvoiceProof.validate() {
			return errors.New("采购项名称、规格、数量或单价无效")
		}
		if price*item.Quantity > 100000000000 {
			return errors.New("单项采购金额超出 MVP 核验范围")
		}
	}
	return nil
}

var lengthSpec = regexp.MustCompile(`(\d+(?:\.\d+)?)(毫米|厘米|米|mm\b|cm\b|m\b)`)

func NormalizeSpec(raw string) string {
	compact := strings.Map(func(c rune) rune {
		if unicode.IsSpace(c) {
			return -1
		}
		return c
	}, strings.ToLower(strings.TrimSpace(raw)))
	runes := []rune(compact)
	for i, c := range runes {
		if (c == 'x' || c == 'ｘ' || c == '*') && i > 0 && i+1 < len(runes) {
			before, after := runes[i-1], runes[i+1]
			if (before >= '0' && before <= '9' || before == 'm' || before == '米') && after >= '0' && after <= '9' {
				runes[i] = '×'
			}
		}
	}
	compact = string(runes)
	return lengthSpec.ReplaceAllStringFunc(compact, func(match string) string {
		parts := lengthSpec.FindStringSubmatch(match)
		value, ok := new(big.Rat).SetString(parts[1])
		if !ok {
			return match
		}
		factor := int64(1)
		switch parts[2] {
		case "厘米", "cm":
			factor = 10
		case "米", "m":
			factor = 1000
		}
		value.Mul(value, new(big.Rat).SetInt64(factor))
		return "[" + value.RatString() + "mm]"
	})
}
func Audit(in Input, budgetOnly bool) (Report, error) {
	if err := in.Validate(); err != nil {
		return Report{}, err
	}
	budget, _ := ParseMoney(in.Budget)
	report := Report{Activity: in.Activity, BudgetMinor: budget, Findings: []Finding{}, Scope: "仅核对提供的资料；人工核验状态由使用者记录，不代表平台已验证材料真实性。未绑定学校正式制度，不构成审批或报销保证。", RulesVersion: "procurement-guard/v2"}
	add := func(code, severity, title, evidence, action string) {
		report.Findings = append(report.Findings, Finding{Code: code, Severity: severity, Title: title, Evidence: evidence, Action: action})
	}
	for _, item := range in.Items {
		price, _ := ParseMoney(item.UnitPrice)
		report.TotalMinor += price * item.Quantity
	}
	if budget <= 0 {
		add("BUDGET_MISSING", "block", "活动预算待补充", "没有有效的正数预算，无法判断是否超预算。", "补充预算金额并记录依据。")
	} else if report.TotalMinor > budget {
		add("OVER_BUDGET", "block", "采购方案超预算", fmt.Sprintf("明细 %s，填写额度 %s，超出 %s", FormatMoney(report.TotalMinor), FormatMoney(budget), FormatMoney(report.TotalMinor-budget)), "压减采购金额，或完成追加预算审批后更新额度。")
	}
	if budgetOnly {
		report.Scope = "仅核算预算与明细金额；未检查规格、审批、校规或票据。"
		return report, nil
	}
	if in.Demo {
		add("DEMO_DATA", "review", "当前为合成演示资料", "演示文本和记录不属于真实审批或供应商材料。", "实际使用前替换成真实或脱敏资料并重新人工核对。")
	}
	if !in.Approved {
		add("APPROVAL_PENDING", "block", "负责人审批尚未确认", "使用者填写的审批状态为未完成。", "按适用流程取得负责人确认。")
	} else if !in.ApprovalProof.ManuallyReviewed() || in.Demo {
		add("APPROVAL_UNVERIFIED", "review", "审批声明尚未人工复核", "勾选已审批只代表使用者声明；未记录完整人工核验信息。", "对照原始审批记录填写引用、复核人和复核时间。")
	}
	if strings.TrimSpace(in.BudgetEvidence) == "" {
		add("BUDGET_EVIDENCE_MISSING", "review", "预算依据未留档", "未提供预算批复文件、链接或记录编号。", "补充可回查的预算审批依据。")
	} else if !in.BudgetProof.ManuallyReviewed() || in.Demo {
		add("BUDGET_EVIDENCE_UNVERIFIED", "review", "预算依据尚未人工复核", "填写文件名、链接或原文不等于已核验真实材料。", "保留原件，对照材料记录人工核验人、时间和引用。")
	}
	for i, item := range in.Items {
		index := i
		itemFinding := func(code, title, evidence, action string) {
			report.Findings = append(report.Findings, Finding{Code: code, Severity: "review", Title: item.Name + "：" + title, Evidence: evidence, Action: action, ItemIndex: &index})
		}
		if strings.TrimSpace(item.QuoteSource) == "" {
			itemFinding("QUOTE_SOURCE_MISSING", "报价来源缺失", "未提供供应商、报价文件或链接。", "补充原始报价的引用位置。")
		} else if !item.QuoteProof.ManuallyReviewed() || in.Demo {
			itemFinding("QUOTE_SOURCE_UNVERIFIED", "报价来源尚未人工复核", "来源标签或粘贴文本未被独立验证，不能作为真实供应商保证。", "对照原始报价记录人工复核信息。")
		}
		if NormalizeSpec(item.RequestedSpec) == "" || NormalizeSpec(item.QuotedSpec) == "" {
			itemFinding("SPEC_MISSING", "规格信息待补充", "需求或报价规格为空。", "联系供应商并补齐可比规格。")
		} else if NormalizeSpec(item.RequestedSpec) != NormalizeSpec(item.QuotedSpec) {
			itemFinding("SPEC_MISMATCH", "需求与报价规格不同", fmt.Sprintf("需求 %q；报价 %q；来源 %q", item.RequestedSpec, item.QuotedSpec, item.QuoteSource), "支持明确标注的 mm/cm/m 长度换算；其他差异请人工确认，不能自动认定替代规格。")
		}
		if !item.InvoiceConfirmed {
			itemFinding("INVOICE_UNCONFIRMED", "票据条件待确认", "使用者尚未记录供应商的开票确认。", "向供应商确认票据类型、抬头及明细。")
		} else if !item.InvoiceProof.ManuallyReviewed() || in.Demo {
			itemFinding("INVOICE_UNVERIFIED", "开票声明尚未人工复核", "填写供应商已确认不等于材料已经人工核对。", "核对供应商原始答复并记录复核人、时间和引用。")
		}
	}
	applyPolicy(in, &report)
	return report, nil
}
