// Copyright 2025 StablePay. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// GiftCodeEntry represents a single gift code in the pool.
type GiftCodeEntry struct {
	Code string `json:"code"`
	Used bool   `json:"used"`
}

// GiftCodeFile is the on-disk JSON structure.
type GiftCodeFile struct {
	GiftCodes []GiftCodeEntry `json:"gift_codes"`
}

// GiftCodeService manages a pool of gift codes loaded from a JSON file.
// When a purchase completes, Allocate() pops an unused code and marks it used.
type GiftCodeService struct {
	mu     sync.Mutex
	path   string
	codes  []GiftCodeEntry
	dirty  bool
}

// NewGiftCodeService loads gift codes from a JSON file.
// If the file doesn't exist, it creates one with placeholder entries.
func NewGiftCodeService(path string) (*GiftCodeService, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("gift code: resolve path: %w", err)
	}

	s := &GiftCodeService{path: absPath}

	data, err := os.ReadFile(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create an empty pool.
			s.codes = []GiftCodeEntry{}
			s.dirty = true
			_ = s.flush()
			return s, nil
		}
		return nil, fmt.Errorf("gift code: read file: %w", err)
	}

	var file GiftCodeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("gift code: parse file: %w", err)
	}
	s.codes = file.GiftCodes
	return s, nil
}

// CountRemaining returns how many unused codes are left.
func (s *GiftCodeService) CountRemaining() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, c := range s.codes {
		if !c.Used {
			count++
		}
	}
	return count
}

// Allocate picks the first unused gift code, marks it used, flushes to disk, and returns it.
// Returns empty string if no codes remain.
func (s *GiftCodeService) Allocate() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i := range s.codes {
		if !s.codes[i].Used {
			s.codes[i].Used = true
			s.dirty = true
			_ = s.flush()
			return s.codes[i].Code
		}
	}
	return ""
}

// flush writes the current state to disk if dirty.
func (s *GiftCodeService) flush() error {
	if !s.dirty {
		return nil
	}
	data, err := json.MarshalIndent(GiftCodeFile{GiftCodes: s.codes}, "", "  ")
	if err != nil {
		return fmt.Errorf("gift code: marshal: %w", err)
	}
	if err := os.WriteFile(s.path, data, 0644); err != nil {
		return fmt.Errorf("gift code: write file: %w", err)
	}
	s.dirty = false
	return nil
}
