package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
)

type GiftCodeEntry struct {
	Code string `json:"code"`
	Used bool   `json:"used"`
}

type GiftCodeFile struct {
	GiftCodes []GiftCodeEntry `json:"gift_codes"`
}

const charset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

func randomCode() string {
	b := make([]byte, 16)
	for i := range b {
		n, _ := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		b[i] = charset[n.Int64()]
	}
	s := string(b)
	return s[:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func main() {
	count := 30
	if len(os.Args) > 1 {
		fmt.Sscanf(os.Args[1], "%d", &count)
	}

	entries := make([]GiftCodeEntry, count)
	for i := range entries {
		entries[i] = GiftCodeEntry{Code: randomCode(), Used: false}
	}

	data := GiftCodeFile{GiftCodes: entries}
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Fatalf("marshal: %v", err)
	}

	out := filepath.Join(".", "data", "gift_codes.json")
	if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
		log.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(out, raw, 0644); err != nil {
		log.Fatalf("write: %v", err)
	}

	fmt.Printf("✅ 已生成 %d 个礼品码 -> %s\n", count, out)
	for _, e := range entries {
		fmt.Println("  " + e.Code)
	}
}
