package agentshopping

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// NaturalizeChallengeTaskPrompts replaces the generator's single-form wording
// with deterministic, varied Chinese shopping requests. It changes only the
// prompt text; intents, decision labels, and SKU labels are left untouched.
func NaturalizeChallengeTaskPrompts(tasks []ChallengeTask, seed int64) error {
	if len(tasks) == 0 {
		return fmt.Errorf("cannot naturalize an empty challenge suite: %w", ErrInvalidEvaluationSuite)
	}
	for index := range tasks {
		task := &tasks[index]
		if strings.TrimSpace(task.TaskID) == "" || strings.TrimSpace(task.Intent.Region) == "" || task.Intent.MaxPriceMinor <= 0 {
			return fmt.Errorf("challenge task %q lacks prompt context: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
		selector := sha256.Sum256([]byte(fmt.Sprintf("natural-challenge-prompt-v1|%d|%s", seed, task.TaskID)))
		context := challengeShoppingContexts[int(selector[0])%len(challengeShoppingContexts)]
		switch task.ExpectedDisposition {
		case ChallengePlan, ChallengeNoPlan:
			task.Prompt = naturalCompleteShoppingPrompt(*task, context, selector)
		case ChallengeClarify:
			task.Prompt = naturalClarificationPrompt(*task, context, selector)
		default:
			return fmt.Errorf("challenge task %s has unsupported disposition %q: %w", task.TaskID, task.ExpectedDisposition, ErrInvalidEvaluationSuite)
		}
		if len(task.Prompt) > 8000 {
			return fmt.Errorf("naturalized prompt for %s exceeds parser limit: %w", task.TaskID, ErrInvalidEvaluationSuite)
		}
	}
	return nil
}

var challengeShoppingContexts = []string{
	"准备给团队做一次小型分享",
	"最近在整理家里的工作区",
	"这是给朋友挑的一份心意",
	"我正在为一个课程项目做准备",
	"想给新办公室补齐常用物品",
	"周末要安排一次简单体验",
	"手头的旧版本快用完了",
	"我在列下个月的采购清单",
	"给客户演示前需要准备材料",
	"最近想把个人资料重新整理一下",
	"准备替家里添置一件实用的东西",
	"这项需求要赶在近期交付前解决",
}

var completeShoppingOpeners = []string{
	"%s：我想找%s，规格要%s。送到%s，预算上限%s USDC。",
	"为%s做准备时，帮我挑%s的%s款；收货地%s，单件不超过%s USDC。",
	"我这边%s，需要一件%s（%s）。配送到%s，预算最多%s USDC。",
	"最近要处理%s，想看看%s的%s规格；送往%s，价格控制在%s USDC以内。",
	"为%s准备采购时，清单里还缺%s，规格%s，地区%s，预算%s USDC。",
	"在%s这个场景下，我想跨店比较%s，筛%s规格，目的地%s，单件最多%s USDC。",
	"帮我给%s找%s的%s款，送到%s，最多花%s USDC。",
	"关于%s，我需要%s（%s），收货地%s，预算不高于%s USDC。",
	"请按%s这个用途，看看%s的%s规格；配送到%s，价格别超过%s USDC。",
	"为了%s，我想在各家店找%s的%s款，送到%s，最多花%s USDC。",
	"为%s采购时，先比较%s的%s规格，地区%s，预算%s USDC以内。",
	"因为%s，能不能帮我选%s？规格看%s，配送到%s，控制在%s USDC以内。",
}

var completeShoppingClosers = []string{
	"只把当前有货且确实能配送的报价列出来；先等我确认再付款。",
	"请同时给出商家、可售库存和当前报价，我明确批准后才能下单。",
	"缺货或超预算的不要放进可购买方案；报价/库存变化时要重新问我。",
	"同规格替代可以列作备选，但请标清差异；没拿到我的确认不能扣款。",
	"我会看过最终卖家和价格再决定，任何报价变化都需要再次确认。",
	"先给候选和可验证的实时报价，别替我自动结账。",
}

func naturalCompleteShoppingPrompt(task ChallengeTask, context string, selector [32]byte) string {
	intent := task.Intent
	region := challengeRegionDisplay(intent.Region)
	format := intent.RequiredFormat
	if format == "" {
		format = "规格合适的"
	}
	item := intent.Item
	if item == "" {
		item = "相关商品"
	}
	price := FormatMinorUnits(intent.MaxPriceMinor)
	opener := completeShoppingOpeners[int(selector[1])%len(completeShoppingOpeners)]
	text := fmt.Sprintf(opener, context, item, format, region, price)
	if attributes := naturalHardAttributeClause(intent.RequiredAttributes); attributes != "" {
		text += " " + attributes
	}
	if style := strings.TrimSpace(intent.PreferredStyle); style != "" {
		styleOptions := []string{
			"软偏好是" + style + "，优先考虑但不要当成硬性筛选。",
			"如果其他条件相当，我更喜欢" + style + "风格。",
			"风格上倾向" + style + "；真正必须满足的仍是上面列出的规格与属性。",
		}
		text += " " + styleOptions[int(selector[2])%len(styleOptions)]
	}
	if intent.AllowSubstitution {
		text += " 同规格替代也可作为备选，请明确标出替代关系。"
	} else {
		text += " 不要自行换成其他商品。"
	}
	text += " " + completeShoppingClosers[int(selector[3])%len(completeShoppingClosers)]
	return text
}

func naturalClarificationPrompt(task ChallengeTask, context string, selector [32]byte) string {
	intent := task.Intent
	category := challengeCategoryDisplay(intent.Category)
	region := challengeRegionDisplay(intent.Region)
	price := FormatMinorUnits(intent.MaxPriceMinor)
	style := ""
	if intent.PreferredStyle != "" {
		style = "风格方面我偏好" + intent.PreferredStyle + "。"
	}
	var prompt string
	switch {
	case intent.Item == "" && intent.RequiredFormat == "":
		openers := []string{
			"%s，我想从%s里选一件合适的东西。",
			"最近要处理这件事：%s。能不能帮我挑个%s类商品？",
			"我在考虑%s，想买一件%s相关的商品。",
		}
		prompt = fmt.Sprintf(openers[int(selector[1])%len(openers)], context, category)
	case intent.Item == "":
		openers := []string{
			"%s，想看看%s类里有没有%s规格的选择。",
			"%s，我需要%s方向的商品，形式上倾向%s。",
			"%s，帮我找点%s相关的东西，最好是%s规格。",
		}
		prompt = fmt.Sprintf(openers[int(selector[1])%len(openers)], context, category, intent.RequiredFormat)
	case intent.RequiredFormat == "":
		openers := []string{
			"%s，我想买%s，有哪些选择比较合适？",
			"%s，最近需要%s，什么规格都可以先看看。",
			"能帮我找一下%s吗？我打算%s，想听听怎么选。",
		}
		prompt = fmt.Sprintf(openers[int(selector[1])%len(openers)], context, intent.Item)
	default:
		return ""
	}
	if style != "" {
		prompt += " " + style
	}
	prompt += fmt.Sprintf("送到%s，预算不超过%s USDC。先给我看商家和当前报价，我确认后再继续；如果价格变化请重新确认。", region, price)
	return prompt
}

func naturalHardAttributeClause(attributes map[string]string) string {
	if len(attributes) == 0 {
		return ""
	}
	keys := make([]string, 0, len(attributes))
	for key := range attributes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+attributes[key])
	}
	openers := []string{
		"商品还必须同时满足这些硬条件：",
		"下面的商品属性不能妥协：",
		"筛选时把这些属性作为必选项：",
	}
	return openers[len(keys)%len(openers)] + strings.Join(parts, "、") + "。"
}

func challengeRegionDisplay(region string) string {
	switch region {
	case "CN-EAST":
		return "华东地区"
	case "CN-NORTH":
		return "华北地区"
	case "CN-SOUTH":
		return "华南地区"
	case "CN-WEST":
		return "西部地区"
	default:
		return region
	}
}

func challengeCategoryDisplay(category string) string {
	labels := map[string]string{
		"audio": "音频内容", "books": "书籍", "cloud-tools": "云端工具", "coffee": "咖啡",
		"design-assets": "设计素材", "developer-tools": "开发工具", "education": "课程",
		"game-collectibles": "数字收藏品", "stock-media": "媒体素材", "templates": "模板",
		"travel-guides": "旅行资料", "wellness-guides": "健康指南",
	}
	if label := labels[category]; label != "" {
		return label
	}
	return category
}
