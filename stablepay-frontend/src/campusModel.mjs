export const workspaceKey = "stablepay-campus-workspace-v2";
export const emptyProof = (reference = "") => ({
  state: reference ? "provided" : "declared",
  reference,
  reviewed_by: "",
  reviewed_at: "",
});
export const requirementsExample =
  "活动名称：社团迎新分享会。活动预算500元，预算记录仅为合成演示，未取得真实审批。\n采购需求：横幅1条，规格3m × 0.8m；展架2个，规格80cm × 180cm；饮用水4箱，规格24瓶/箱；桌牌4个，规格亚克力A5。";
export const quotationsExample =
  "以下是合成报价，用于演示，不是真实供应商材料。\n演示供应商A：横幅，报价规格300cm × 80cm，单价180元，可提供发票。\n演示供应商B：展架，报价规格60cm × 160cm，单价68元，可提供发票。\n演示供应商C：饮用水，报价规格24瓶/箱，单价35元，开票条件未确认。\n演示供应商D：桌牌，报价规格亚克力A5，单价20元，可提供发票。";
export function newItem() {
  return {
    name: "",
    requested_spec: "",
    quoted_spec: "",
    quantity: "",
    unit_price: "",
    quote_source: "",
    invoice_confirmed: false,
    quote_proof: emptyProof(),
    invoice_proof: emptyProof(),
  };
}
export function initialDraft() {
  return {
    activity: "社团迎新分享会",
    school: "",
    budget: "500",
    budget_evidence: "合成演示预算记录，非真实审批材料",
    approved: false,
    demo: true,
    budget_proof: emptyProof("合成演示预算记录"),
    approval_proof: emptyProof(),
    items: [
      ["横幅", "3m × 0.8m", "300cm × 80cm", 1, "180"],
      ["展架", "80cm × 180cm", "60cm × 160cm", 2, "68"],
      ["饮用水", "24瓶/箱", "24瓶/箱", 4, "35"],
      ["桌牌", "亚克力A5", "亚克力A5", 4, "20"],
    ].map(([name, requested_spec, quoted_spec, quantity, unit_price]) => ({
      ...newItem(),
      name,
      requested_spec,
      quoted_spec,
      quantity,
      unit_price,
      quote_source: "合成演示报价，非真实来源",
      quote_proof: emptyProof("合成演示报价"),
    })),
  };
}
export function moneyMinor(value) {
  const raw = String(value).trim();
  if (!/^\d{1,9}(?:\.\d{1,2})?$/.test(raw)) return null;
  const [whole, fraction = ""] = raw.split(".");
  return Number(whole) * 100 + Number(fraction.padEnd(2, "0"));
}
export function previewTotal(items) {
  let total = 0;
  for (const item of items) {
    const price = moneyMinor(item.unit_price);
    const quantity = Number(item.quantity);
    if (
      price === null ||
      price <= 0 ||
      !Number.isSafeInteger(quantity) ||
      quantity <= 0 ||
      quantity > 100000 ||
      price * quantity > 100000000000
    )
      return null;
    total += price * quantity;
  }
  return items.length ? total : null;
}
export function reviewPayload(draft, extractionID, confirmed) {
  if (!confirmed) throw new Error("请先核对并确认当前清单。");
  if (!draft.activity.trim() || !draft.items.length || draft.items.length > 50)
    throw new Error("请填写活动名称和1–50项采购明细。");
  if (draft.budget.trim() && moneyMinor(draft.budget) === null)
    throw new Error("预算须为非负金额，最多两位小数。");
  if (
    previewTotal(draft.items) === null ||
    draft.items.some((item) => !item.name.trim())
  )
    throw new Error(
      "请补齐名称、正整数数量和有效正数单价；未知信息不能按零元计算。",
    );
  return {
    input: {
      ...draft,
      items: draft.items.map((item) => ({
        ...item,
        quantity: Number(item.quantity),
      })),
    },
    extraction_id: extractionID || "",
    confirmed: true,
  };
}
export function sourceExcerpt(source, field) {
  const chars = Array.from(source);
  if (chars.slice(field.start_rune, field.end_rune).join("") !== field.quote)
    return null;
  return {
    before: chars
      .slice(Math.max(0, field.start_rune - 12), field.start_rune)
      .join(""),
    quote: field.quote,
    after: chars.slice(field.end_rune, field.end_rune + 12).join(""),
  };
}
export function isCurrentReview(review, draft) {
  return Boolean(
    review && JSON.stringify(review.input) === JSON.stringify(draft),
  );
}
