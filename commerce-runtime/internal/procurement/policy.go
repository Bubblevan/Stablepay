package procurement

import "strings"

type PolicyCheck struct {
	Section string `json:"section"`
	Summary string `json:"summary"`
}
type PolicyReference struct {
	ID        string        `json:"id"`
	School    string        `json:"school"`
	Title     string        `json:"title"`
	Version   string        `json:"version"`
	URL       string        `json:"url"`
	Scope     string        `json:"scope"`
	CheckedOn string        `json:"checked_on"`
	Applied   bool          `json:"applied"`
	Checks    []PolicyCheck `json:"checks"`
}

func PolicyCatalog() []PolicyReference {
	return []PolicyReference{{ID: "cup-club-fund-20260327", School: "中国石油大学（北京）", Title: "学生社团活动经费申请审批工作管理细则", Version: "官网发布版 2026-03-27", CheckedOn: "2026-10-28", URL: "https://www.cup.edu.cn/tuanwei/202606xg/szyxxt/stjszdb/dcb68c41e725455ca23bb4dfb3960670.htm", Scope: "该校注册校级学生社团申请使用校团委社团活动专项经费。仅提供部分条款提示，不覆盖其他单位、资金来源或完整报销要求。", Checks: []PolicyCheck{{Section: "第四、五部分", Summary: "核对预算是否在活动前依流程申请并获得批复；申请需预留至少10个工作日。"}, {Section: "第六部分", Summary: "活动后结算有10天提交期限；人工核对发票、采购与付款记录等实际材料。"}, {Section: "第六、七部分", Summary: "票据抬头、开票时间、明细及其他限制须对照完整原文核查，不能仅勾选可开票。"}}}}
}
func applyPolicy(in Input, report *Report) {
	if in.PolicyID == "" {
		return
	}
	for _, policy := range PolicyCatalog() {
		if policy.ID != in.PolicyID {
			continue
		}
		policy.Applied = in.School == policy.School && in.PolicyScopeConfirmed
		report.Policy = &policy
		if !policy.Applied {
			report.Findings = append(report.Findings, Finding{Code: "POLICY_SCOPE_UNCONFIRMED", Severity: "review", Title: "校规适用范围尚未确认", Evidence: "所选规则只适用于特定学校、社团和资金来源。", Action: "核对学校名称及经费适用范围；不适用时取消该规则包。"})
			return
		}
		report.Scope = "已选择一份学校官网规则参考包，只输出部分条款待办；未核验文件真实性、完整校规或报销资格。人工核验状态是使用者记录。"
		sections := []string{}
		for _, check := range policy.Checks {
			sections = append(sections, check.Section+"："+check.Summary)
		}
		report.Findings = append(report.Findings, Finding{Code: "POLICY_MANUAL_REVIEW", Severity: "review", Title: "所选校规仍有条款需人工确认", Evidence: strings.Join(sections, "；"), Action: "对照所附官网原文及当前校内通知完成核查；本工具不自动判定日期、票据与适用资格。"})
		return
	}
}
