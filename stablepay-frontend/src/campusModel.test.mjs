import test from "node:test";
import assert from "node:assert/strict";
import {
  initialDraft,
  emptyProof,
  moneyMinor,
  previewTotal,
  reviewPayload,
  sourceExcerpt,
  isCurrentReview,
} from "./campusModel.mjs";

test("integer cents avoid floating rounding and reject invalid amounts", () => {
  assert.equal(moneyMinor("0.29"), 29);
  for (const value of ["-1", "0.001", "1e2", "NaN", ""])
    assert.equal(moneyMinor(value), null);
  assert.equal(previewTotal(initialDraft().items), 53600);
});
test("missing price and invalid quantities cannot become free purchases", () => {
  for (const [key, value] of [
    ["quantity", "0"],
    ["quantity", "1.5"],
    ["quantity", "-1"],
    ["unit_price", ""],
    ["unit_price", "-1"],
  ]) {
    const draft = initialDraft();
    draft.items[0][key] = value;
    assert.throws(() => reviewPayload(draft, "", true));
  }
});
test("human confirmation is required, unknown budget remains unknown", () => {
  const draft = initialDraft();
  assert.throws(() => reviewPayload(draft, "", false));
  draft.budget = "";
  assert.equal(reviewPayload(draft, "extraction-id", true).input.budget, "");
  assert.equal(reviewPayload(draft, "", true).input.approved, false);
  assert.equal(emptyProof("file.pdf").state, "provided");
});
test("edits invalidate prior results and export eligibility", () => {
  const draft = initialDraft(),
    review = { input: structuredClone(draft) };
  assert.equal(isCurrentReview(review, draft), true);
  draft.items[0].unit_price = "140";
  assert.equal(isCurrentReview(review, draft), false);
});
test("Unicode source references use rune positions and must match exactly", () => {
  const source = "📦横幅单价180元";
  assert.equal(
    sourceExcerpt(source, { start_rune: 3, end_rune: 8, quote: "单价180" })
      .quote,
    "单价180",
  );
  assert.equal(
    sourceExcerpt(source, { start_rune: 0, end_rune: 2, quote: "伪造" }),
    null,
  );
});
