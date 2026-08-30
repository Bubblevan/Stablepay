package service

import (
	"fmt"
	"strings"
	"time"

	"stablepay-x-verify-hertz/internal/model"
	"stablepay-x-verify-hertz/internal/store"
	"stablepay-x-verify-hertz/internal/util"

	"github.com/google/uuid"
)

type VerifyTwitterService struct {
	didStore     *store.DIDStore
	tweetStore   *store.TweetStore
	bindingStore *store.BindingStore
	rewardStore  *store.RewardStore
}

func NewVerifyTwitterService(
	didStore *store.DIDStore,
	tweetStore *store.TweetStore,
	bindingStore *store.BindingStore,
	rewardStore *store.RewardStore,
) *VerifyTwitterService {
	return &VerifyTwitterService{
		didStore:     didStore,
		tweetStore:   tweetStore,
		bindingStore: bindingStore,
		rewardStore:  rewardStore,
	}
}

func (s *VerifyTwitterService) Verify(req model.VerifyTwitterRequest) (model.VerifyTwitterResponse, *model.ErrorResponse) {
	if !util.IsValidDID(req.DID) {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "invalid_did",
			Message: "did format is invalid",
		}
	}

	_, ok := s.didStore.Get(req.DID)
	if !ok {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "did_not_found",
			Message: "did not found, create wallet first",
		}
	}

	handle, _, normalizedURL, err := util.ParseTweetURL(req.TweetURL)
	if err != nil {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "invalid_tweet_url",
			Message: err.Error(),
		}
	}

	if existingBinding, exists := s.bindingStore.GetByDID(req.DID); exists {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "did_already_bound",
			Message: fmt.Sprintf("did already bound to @%s", existingBinding.TwitterHandle),
		}
	}

	if existingDID, exists := s.bindingStore.GetDIDByHandle(handle); exists {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "twitter_already_bound",
			Message: fmt.Sprintf("@%s already bound to %s", handle, existingDID),
		}
	}

	tweet, ok := s.tweetStore.Get(normalizedURL)
	if !ok {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "tweet_not_found",
			Message: "mock tweet not found, seed it first via /api/v1/mock/twitter/tweets",
		}
	}

	if !tweet.IsPublic {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "tweet_not_public",
			Message: "tweet must be public",
		}
	}

	if util.NormalizeTwitterHandle(tweet.AuthorHandle) != handle {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "tweet_author_mismatch",
			Message: "tweet_url handle does not match stored tweet author_handle",
		}
	}

	expected := util.BuildExpectedVerificationText(req.DID)
	if !strings.Contains(tweet.Text, req.DID) || !strings.Contains(tweet.Text, expected) {
		return model.VerifyTwitterResponse{}, &model.ErrorResponse{
			Success: false,
			Code:    "did_not_in_tweet",
			Message: "tweet text does not contain required DID verification content",
		}
	}

	rewardTx := "reward_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	verifiedAt := time.Now().UTC()

	s.bindingStore.Save(model.Binding{
		DID:           req.DID,
		TwitterHandle: handle,
		TweetURL:      normalizedURL,
		RewardTx:      rewardTx,
		VerifiedAt:    verifiedAt,
	})

	didIdentity, _ := s.didStore.Get(req.DID)
	s.rewardStore.Save(model.RewardRecord{
		DID:        req.DID,
		Amount:     1,
		Currency:   "USDC",
		RewardTx:   rewardTx,
		IssuedAt:   verifiedAt,
		WalletAddr: didIdentity.WalletAddress,
	})

	return model.VerifyTwitterResponse{
		Success:       true,
		TwitterHandle: "@" + handle,
		RewardTx:      rewardTx,
		Message:       "Verification successful, 1 USDC sent",
	}, nil
}
