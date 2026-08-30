package store

import (
	"sync"

	"stablepay-x-verify-hertz/internal/model"
)

type TweetStore struct {
	mu   sync.RWMutex
	data map[string]model.MockTweet
}

func NewTweetStore() *TweetStore {
	return &TweetStore{data: make(map[string]model.MockTweet)}
}

func (s *TweetStore) Save(tweet model.MockTweet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[tweet.TweetURL] = tweet
}

func (s *TweetStore) Get(tweetURL string) (model.MockTweet, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.data[tweetURL]
	return v, ok
}
