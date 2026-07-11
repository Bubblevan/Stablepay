package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"verification-service/kitex_gen/stablepay/common"
	"verification-service/kitex_gen/stablepay/verification_service"

	oauth1 "github.com/dghubble/oauth1"
	"gorm.io/gorm"
)

// XAPIClient X API 客户端
type XAPIClient struct {
	client *http.Client
}

var xClient *XAPIClient

// TweetResult 推文结果
type TweetResult struct {
	Content    string
	AuthorID   string
	AuthorName string
}

func initXAPIClient(apiKey, apiSecret, accessToken, accessTokenSecret string) {
	if apiKey == "" || apiSecret == "" || accessToken == "" || accessTokenSecret == "" {
		log.Printf("【X API】凭证未完整配置，生产环境将无法完成 X 验证")
		return
	}
	config := oauth1.NewConfig(apiKey, apiSecret)
	token := oauth1.NewToken(accessToken, accessTokenSecret)
	httpClient := config.Client(context.Background(), token)
	xClient = &XAPIClient{client: httpClient}
	log.Printf("【X API】客户端已初始化")
}

func allowXVerificationMock() bool {
	return strings.ToLower(envOrDefault("ALLOW_X_VERIFICATION_MOCK", "false")) == "true"
}

// GetTweet 获取推文内容（生产路径：必须成功返回真实推文）
func (x *XAPIClient) GetTweet(tweetID string) (*TweetResult, error) {
	url := "https://api.twitter.com/2/tweets/" + tweetID + "?tweet.fields=text,author_id&expansions=author_id&user.fields=username"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := x.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("tweet not found")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("x api status %d: %s", resp.StatusCode, string(body))
	}

	var tweetResponse struct {
		Data struct {
			ID       string `json:"id"`
			Text     string `json:"text"`
			AuthorID string `json:"author_id"`
		} `json:"data"`
		Includes struct {
			Users []struct {
				ID       string `json:"id"`
				Username string `json:"username"`
			} `json:"users"`
		} `json:"includes"`
	}

	if err := json.Unmarshal(body, &tweetResponse); err != nil {
		return nil, fmt.Errorf("x api json parse failed: %w", err)
	}
	if tweetResponse.Data.Text == "" {
		return nil, fmt.Errorf("tweet content empty")
	}

	result := &TweetResult{
		Content:  tweetResponse.Data.Text,
		AuthorID: tweetResponse.Data.AuthorID,
	}
	for _, user := range tweetResponse.Includes.Users {
		if user.ID == tweetResponse.Data.AuthorID || result.AuthorName == "" {
			result.AuthorName = user.Username
		}
	}

	log.Printf("【X API】推文 @%s: %s", result.AuthorName, result.Content)
	return result, nil
}

func strPtr(s string) *string { return &s }
func i64Ptr(i int64) *int64  { return &i }

// VerificationServiceImpl implements the last service interface defined in the IDL.
type VerificationServiceImpl struct{}

func (s *VerificationServiceImpl) VerifyPurchase(ctx context.Context, req *verification_service.VerifyPurchaseRequest) (resp *verification_service.VerifyPurchaseResponse, err error) {
	log.Printf("👉 【服务端】收到请求参数: AgentDid='%s', SkillDid='%s'", req.AgentDid, req.SkillDid)

	resp = verification_service.NewVerifyPurchaseResponse()
	var record PurchaseRecord

	result := DB.Where("agent_did = ? AND skill_did = ?", req.AgentDid, req.SkillDid).First(&record)

	if result.Error == nil {
		log.Printf("【服务端】查到数据！真实流水号: %s", record.TxId)
		resp.Purchased = true
		purchaseTime := record.CreatedAt.Format(time.RFC3339)
		resp.PurchaseTime = &purchaseTime
		txId := common.TxId(record.TxId)
		resp.TxId = &txId
		resp.Base = &common.BaseResp{Code: 0, Message: "success"}
	} else {
		log.Printf("【服务端】数据库报错信息: %v", result.Error)
		resp.Purchased = false
		resp.Base = &common.BaseResp{Code: 10001, Message: "record not found"}
	}

	return resp, nil
}

// BatchVerifyPurchase 批量校验一个 agent 的多个 skill 是否已购买。
// 对每个 skill_did 查一次 PurchaseRecord,按顺序返回 BatchVerifyItem。
func (s *VerificationServiceImpl) BatchVerifyPurchase(ctx context.Context, req *verification_service.BatchVerifyPurchaseRequest) (resp *verification_service.BatchVerifyPurchaseResponse, err error) {
	resp = verification_service.NewBatchVerifyPurchaseResponse()
	agentDid := strings.TrimSpace(string(req.AgentDid))

	if agentDid == "" {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_PARAMETERS),
			Message: "agent_did 必填",
		}
		return resp, nil
	}
	if len(req.SkillDids) == 0 {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_PARAMETERS),
			Message: "skill_dids 至少一个",
		}
		return resp, nil
	}

	items := make([]*verification_service.BatchVerifyItem, 0, len(req.SkillDids))
	for _, skillDid := range req.SkillDids {
		item := verification_service.NewBatchVerifyItem()
		item.SkillDid = skillDid
		item.Purchased = false

		var record PurchaseRecord
		result := DB.Where("agent_did = ? AND skill_did = ?", agentDid, string(skillDid)).First(&record)
		if result.Error == nil {
			item.Purchased = true
			purchaseTime := record.CreatedAt.Format(time.RFC3339)
			item.PurchaseTime = &purchaseTime
			txId := common.TxId(record.TxId)
			item.TxId = &txId
		} else if result.Error != gorm.ErrRecordNotFound {
			log.Printf("【服务端】BatchVerifyPurchase 查询失败: did=%s skill=%s err=%v", agentDid, skillDid, result.Error)
		}

		items = append(items, item)
	}

	resp.Items = items
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}
	log.Printf("【服务端】BatchVerifyPurchase 完成: agent=%s items=%d", agentDid, len(items))
	return resp, nil
}

// GetPurchaseProof 取单个 (agent_did, skill_did) 的购买凭证。
// 命中 PurchaseRecord 则返回 purchased=true + tx_id + purchase_time + proof_version;
// 未命中则 purchased=false + code=10001。
// 注:AmountMinor/Currency/TxHash 需要跨 RPC 反查 payment-service 才能填,本次不实现,留 TODO。
func (s *VerificationServiceImpl) GetPurchaseProof(ctx context.Context, req *verification_service.GetPurchaseProofRequest) (resp *verification_service.GetPurchaseProofResponse, err error) {
	resp = verification_service.NewGetPurchaseProofResponse()
	agentDid := strings.TrimSpace(string(req.AgentDid))
	skillDid := strings.TrimSpace(string(req.SkillDid))

	if agentDid == "" || skillDid == "" {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_PARAMETERS),
			Message: "agent_did、skill_did 均为必填",
		}
		return resp, nil
	}

	var record PurchaseRecord
	result := DB.Where("agent_did = ? AND skill_did = ?", agentDid, skillDid).First(&record)
	if result.Error != nil {
		log.Printf("【服务端】GetPurchaseProof 未命中: did=%s skill=%s err=%v", agentDid, skillDid, result.Error)
		resp.Purchased = false
		resp.Base = &common.BaseResp{Code: 10001, Message: "record not found"}
		return resp, nil
	}

	resp.Purchased = true
	purchaseTime := record.CreatedAt.Format(time.RFC3339)
	resp.PurchaseTime = &purchaseTime
	txId := common.TxId(record.TxId)
	resp.TxId = &txId
	proofVersion := "v1"
	resp.ProofVersion = &proofVersion
	resp.Base = &common.BaseResp{Code: 0, Message: "success"}

	// TODO: 反查 payment-service 填充 AmountMinor / Currency / TxHash
	return resp, nil
}

func (s *VerificationServiceImpl) VerifyXTweet(ctx context.Context, req *verification_service.VerifyXTweetRequest) (resp *verification_service.VerifyXTweetResponse, err error) {
	log.Printf("👉 【服务端】收到 X 验证请求: AgentDid='%s', WalletAddress='%s', TweetUrl='%s'", req.AgentDid, req.WalletAddress, req.TweetUrl)

	resp = verification_service.NewVerifyXTweetResponse()
	agentDid := strings.TrimSpace(string(req.AgentDid))
	walletAddress := strings.TrimSpace(req.WalletAddress)

	if agentDid == "" || walletAddress == "" || strings.TrimSpace(req.TweetUrl) == "" {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_PARAMETERS),
			Message: "agent_did、wallet_address、tweet_url 均为必填",
		}
		resp.Verified = false
		return resp, nil
	}

	tweetID := extractTweetID(req.TweetUrl)
	if tweetID == "" {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_TWEET_URL),
			Message: "无效的推文链接",
		}
		resp.Verified = false
		return resp, nil
	}

	var tweetResult *TweetResult
	if xClient != nil {
		tweetResult, err = xClient.GetTweet(tweetID)
		if err != nil {
			log.Printf("【服务端】X API 调用失败: %v", err)
			if allowXVerificationMock() {
				tweetResult = mockTweetResult(agentDid, walletAddress)
			} else {
				code := common.ErrorCode_TWEET_NOT_FOUND
				msg := "无法获取推文，请确认链接公开可见"
				if strings.Contains(strings.ToLower(err.Error()), "not found") {
					code = common.ErrorCode_TWEET_NOT_FOUND
				} else {
					code = common.ErrorCode_TWEET_NOT_VERIFIED
					msg = "X API 验证失败，请稍后重试"
				}
				resp.Base = &common.BaseResp{Code: int32(code), Message: msg}
				resp.Verified = false
				return resp, nil
			}
		}
	} else if allowXVerificationMock() {
		log.Printf("【服务端】X API 未配置，ALLOW_X_VERIFICATION_MOCK=true，使用本地 mock 推文")
		tweetResult = mockTweetResult(agentDid, walletAddress)
	} else {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_SERVICE_UNAVAILABLE),
			Message: "X API 未配置，无法完成验证",
		}
		resp.Verified = false
		return resp, nil
	}

	if !tweetContainsDid(tweetResult.Content, agentDid) {
		log.Printf("【服务端】推文中未找到 DID: %s", agentDid)
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_WALLET_NOT_FOUND_IN_TWEET),
			Message: "推文中未找到您的 DID，请确保推文包含完整的 did:solana:...",
		}
		resp.Verified = false
		return resp, nil
	}

	var existingByAgent XVerification
	result := DB.Where("agent_did = ? AND verified = ?", agentDid, true).First(&existingByAgent)
	if result.Error == nil {
		return s.alreadyClaimedResponse(agentDid, resp), nil
	}

	if tweetResult.AuthorName != "" && tweetResult.AuthorName != "unknown" {
		var existingByX XVerification
		result := DB.Where("x_username = ? AND verified = ?", tweetResult.AuthorName, true).First(&existingByX)
		if result.Error == nil && existingByX.AgentDid != agentDid {
			resp.Base = &common.BaseResp{
				Code:    int32(common.ErrorCode_X_ACCOUNT_ALREADY_BOUND),
				Message: "此 X 账号已被其他 DID 绑定，每个 X 账号只能绑定一个 DID",
			}
			resp.Verified = false
			return resp, nil
		}
	}

	rewardAmount := int64(1000000)
	idempotencyKey := "x-reward-" + agentDid + "-" + tweetID
	payout, payoutErr := sendRegistrationReward(ctx, agentDid, walletAddress, rewardAmount, idempotencyKey)
	if payoutErr != nil {
		log.Printf("【服务端】注册奖励发放失败: %v", payoutErr)
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_BLOCKCHAIN_NETWORK_ERROR),
			Message: "验证通过但奖励发放失败，请稍后重试",
		}
		resp.Verified = false
		return resp, nil
	}

	verification := XVerification{
		AgentDid:      agentDid,
		WalletAddress: walletAddress,
		XUsername:     tweetResult.AuthorName,
		TweetUrl:      req.TweetUrl,
		TweetContent:  tweetResult.Content,
		Verified:      true,
		RewardAmount:  rewardAmount,
		RewardTxId:    payout.TxID,
		VerifiedAt:    time.Now(),
	}

	if err := DB.Create(&verification).Error; err != nil {
		// DB 唯一索引兜底:并发请求里两个 goroutine 都过了上面的"先查"检查,只有一个能 Create 成功。
		// 另一个会撞上 unique 约束,这时应该返回"已领取"而不是 500。
		if errors.Is(err, gorm.ErrDuplicatedKey) {
			log.Printf("【服务端】X 验证并发命中 unique 约束,agent=%s 当作已领取处理", agentDid)
			return s.alreadyClaimedResponse(agentDid, resp), nil
		}
		log.Printf("【服务端】保存验证记录失败: %v", err)
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INTERNAL_SERVER_ERROR),
			Message: "服务器内部错误",
		}
		resp.Verified = false
		return resp, nil
	}

	log.Printf("【服务端】X 验证成功！奖励 tx=%s hash=%s", payout.TxID, payout.TxHash)

	resp.Base = &common.BaseResp{Code: 0, Message: "验证成功！已发放奖励"}
	resp.Verified = true
	resp.Message = strPtr(fmt.Sprintf("验证成功！已绑定 @%s，1 USDC 奖励已发放", tweetResult.AuthorName))
	resp.RewardAmountMinor = i64Ptr(rewardAmount)
	resp.RewardTxId = &payout.TxID

	return resp, nil
}

func (s *VerificationServiceImpl) GetXVerificationStatus(ctx context.Context, req *verification_service.GetXVerificationStatusRequest) (resp *verification_service.GetXVerificationStatusResponse, err error) {
	log.Printf("👉 【服务端】收到查询 X 验证状态请求: AgentDid='%s'", req.AgentDid)

	resp = verification_service.NewGetXVerificationStatusResponse()
	agentDid := strings.TrimSpace(string(req.AgentDid))
	if agentDid == "" {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_INVALID_PARAMETERS),
			Message: "agent_did 必填",
		}
		resp.Verified = false
		return resp, nil
	}

	var verification XVerification
	result := DB.Where("agent_did = ?", agentDid).First(&verification)

	if result.Error != nil {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_RESOURCE_NOT_FOUND),
			Message: "未找到 X 验证记录",
		}
		resp.Verified = false
		return resp, nil
	}

	verifiedAtStr := verification.VerifiedAt.Format(time.RFC3339)

	resp.Base = &common.BaseResp{Code: 0, Message: "查询成功"}
	resp.Verified = verification.Verified
	resp.TweetUrl = &verification.TweetUrl
	resp.VerifiedAt = &verifiedAtStr
	resp.RewardAmountMinor = i64Ptr(verification.RewardAmount)
	resp.RewardTxId = &verification.RewardTxId

	return resp, nil
}

func mockTweetResult(agentDid, walletAddress string) *TweetResult {
	_ = walletAddress
	return &TweetResult{
		Content:    "I'm verifying my StablePay DID: " + agentDid + "\n\nJoin me on @StablePay!",
		AuthorName: "mock_user",
	}
}

func tweetContainsDid(content, agentDid string) bool {
	content = strings.TrimSpace(content)
	did := strings.TrimSpace(agentDid)
	if content == "" || did == "" {
		return false
	}
	if strings.Contains(content, did) {
		return true
	}
	// Allow shortened display forms if user omits prefix in tweet UI
	if strings.HasPrefix(did, "did:solana:") {
		suffix := strings.TrimPrefix(did, "did:solana:")
		return suffix != "" && strings.Contains(content, suffix)
	}
	return false
}

func extractTweetID(url string) string {
	patterns := []string{
		`twitter\.com/[^/]+/status/(\d+)`,
		`x\.com/[^/]+/status/(\d+)`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		match := re.FindStringSubmatch(url)
		if len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

// alreadyClaimedResponse 复用"已领取"成功响应的装配逻辑。
// 用于两条路径:
//  1. 入口处"先查"发现已有 verified=true 记录,直接返回;
//  2. 并发场景下 Create 撞 unique 约束时,先回查一次再返回。
func (s *VerificationServiceImpl) alreadyClaimedResponse(agentDid string, resp *verification_service.VerifyXTweetResponse) *verification_service.VerifyXTweetResponse {
	var existing XVerification
	if err := DB.Where("agent_did = ? AND verified = ?", agentDid, true).First(&existing).Error; err == nil {
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_VERIFICATION_ALREADY_CLAIMED),
			Message: "您已经完成 X 验证并领取奖励",
		}
		resp.Verified = true
		resp.RewardAmountMinor = i64Ptr(existing.RewardAmount)
		resp.RewardTxId = &existing.RewardTxId
		resp.Message = strPtr("已领取过奖励")
	} else {
		// 兜底:unique 刚撞上时,对手可能还没 commit,回查没找到时仍当"已领取"回包,
		// 不暴露内部错误给 verification 客户端。
		log.Printf("【服务端】unique 冲突后回查未命中: agent=%s err=%v", agentDid, err)
		resp.Base = &common.BaseResp{
			Code:    int32(common.ErrorCode_VERIFICATION_ALREADY_CLAIMED),
			Message: "您已经完成 X 验证并领取奖励",
		}
		resp.Verified = true
		resp.Message = strPtr("已领取过奖励")
	}
	return resp
}
