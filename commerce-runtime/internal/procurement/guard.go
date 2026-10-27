// Package procurement is the shared, deterministic pre-purchase guard used by
// the PAYATHON merchant service and the campus competition's review endpoint.
package procurement

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type Item struct {
	Name             string `json:"name"`
	RequestedSpec    string `json:"requested_spec"`
	QuotedSpec       string `json:"quoted_spec"`
	Quantity         int64  `json:"quantity"`
	UnitPrice        string `json:"unit_price"`
	QuoteSource      string `json:"quote_source"`
	InvoiceConfirmed bool   `json:"invoice_confirmed"`
}
type Input struct {
	Activity       string `json:"activity"`
	School         string `json:"school"`
	Budget         string `json:"budget"`
	BudgetEvidence string `json:"budget_evidence"`
	Approved       bool   `json:"approved"`
	Items          []Item `json:"items"`
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
	Activity     string    `json:"activity"`
	BudgetMinor  int64     `json:"budget_minor"`
	TotalMinor   int64     `json:"total_minor"`
	Findings     []Finding `json:"findings"`
	Scope        string    `json:"scope"`
	RulesVersion string    `json:"rules_version"`
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
	if strings.TrimSpace(in.Activity) == "" || len(in.Activity) > 200 || len(in.School) > 200 || len(in.BudgetEvidence) > 1000 || len(in.Items) == 0 || len(in.Items) > 50 {
		return errors.New("请填写活动名称及 1–50 项采购明细")
	}
	budget, err := ParseMoney(in.Budget)
	if err != nil || budget <= 0 {
		return errors.New("请填写有效活动预算")
	}
	for _, item := range in.Items {
		price, err := ParseMoney(item.UnitPrice)
		if err != nil || price <= 0 || item.Quantity <= 0 || item.Quantity > 100000 || strings.TrimSpace(item.Name) == "" || len(item.Name) > 200 || len(item.RequestedSpec) > 500 || len(item.QuotedSpec) > 500 || len(item.QuoteSource) > 1000 {
			return errors.New("采购项名称、规格、数量或单价无效")
		}
		if price*item.Quantity > 100000000000 {
			return errors.New("单项采购金额超出 MVP 核验范围")
		}
	}
	return nil
}
func normalizeSpec(raw string) string {
	return strings.Map(func(c rune) rune {
		if c == ' ' || c == '\t' || c == '\n' || c == '×' || c == '*' {
			return -1
		}
		return c
	}, strings.ToLower(strings.TrimSpace(raw)))
}
func Audit(in Input, budgetOnly bool) (Report, error) {
	if err := in.Validate(); err != nil {
		return Report{}, err
	}
	budget, _ := ParseMoney(in.Budget)
	report := Report{Activity: in.Activity, BudgetMinor: budget, Findings: []Finding{}, Scope: "仅核对提供的事实；未绑定学校正式制度，不构成报销或审批保证。", RulesVersion: "procurement-guard/v1"}
	for _, item := range in.Items {
		price, _ := ParseMoney(item.UnitPrice)
		report.TotalMinor += price * item.Quantity
	}
	if report.TotalMinor > budget {
		report.Findings = append(report.Findings, Finding{Code: "OVER_BUDGET", Severity: "block", Title: "采购方案超预算", Evidence: fmt.Sprintf("明细 %s，批准额度 %s，超出 %s", FormatMoney(report.TotalMinor), FormatMoney(budget), FormatMoney(report.TotalMinor-budget)), Action: "压减采购金额，或完成追加预算审批后更新额度。"})
	}
	if budgetOnly {
		report.Scope = "仅核算预算与明细金额；未检查规格、审批、校规或票据。"
		return report, nil
	}
	if !in.Approved {
		report.Findings = append(report.Findings, Finding{Code: "APPROVAL_PENDING", Severity: "block", Title: "负责人审批尚未确认", Evidence: "使用者填写的审批状态为未完成。", Action: "按适用流程取得负责人确认。"})
	}
	if strings.TrimSpace(in.BudgetEvidence) == "" {
		report.Findings = append(report.Findings, Finding{Code: "BUDGET_EVIDENCE_MISSING", Severity: "review", Title: "预算依据未留档", Evidence: "未提供预算批复文件、链接或记录编号。", Action: "补充可回查的预算审批依据。"})
	}
	for i, item := range in.Items {
		index := i
		add := func(code, title, evidence, action string) {
			report.Findings = append(report.Findings, Finding{Code: code, Severity: "review", Title: item.Name + "：" + title, Evidence: evidence, Action: action, ItemIndex: &index})
		}
		if strings.TrimSpace(item.QuoteSource) == "" {
			add("QUOTE_SOURCE_MISSING", "报价来源缺失", "未提供供应商、报价文件或链接。", "补充原始报价的引用位置。")
		}
		if normalizeSpec(item.RequestedSpec) == "" || normalizeSpec(item.QuotedSpec) == "" {
			add("SPEC_MISSING", "规格信息待补充", "需求或报价规格为空。", "联系供应商并补齐可比规格。")
		} else if normalizeSpec(item.RequestedSpec) != normalizeSpec(item.QuotedSpec) {
			add("SPEC_MISMATCH", "需求与报价规格不同", fmt.Sprintf("需求 %q；报价 %q；来源 %q", item.RequestedSpec, item.QuotedSpec, item.QuoteSource), "重新获取所需规格报价，或请负责人确认替代规格。")
		}
		if !item.InvoiceConfirmed {
			add("INVOICE_UNCONFIRMED", "票据条件待确认", "使用者尚未记录供应商的开票确认。", "按本次经费要求向供应商确认票据类型、抬头及明细。")
		}
	}
	return report, nil
}
