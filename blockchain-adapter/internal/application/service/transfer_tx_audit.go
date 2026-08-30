// Package service — debug audit for base64-encoded Solana transactions (legacy message).
package service

import (
	"crypto/sha256"
	"encoding/hex"
	"log"

	"github.com/gagliardetto/solana-go"
)

// LogBase64TxAudit logs fee payer, SHA256 of base64 string, prefix, and first SPL Token Transfer (type 3) accounts if present.
func LogBase64TxAudit(contextLabel string, base64Tx string) {
	if base64Tx == "" {
		log.Printf("[tx_audit] %s: (empty tx)", contextLabel)
		return
	}
	sum := sha256.Sum256([]byte(base64Tx))
	prefixLen := 80
	if len(base64Tx) < prefixLen {
		prefixLen = len(base64Tx)
	}
	log.Printf("[tx_audit] %s: b64_len=%d sha256_of_b64_utf8=%s b64_prefix=%q",
		contextLabel, len(base64Tx), hex.EncodeToString(sum[:]), base64Tx[:prefixLen])

	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(base64Tx); err != nil {
		log.Printf("[tx_audit] %s: unmarshal error: %v", contextLabel, err)
		return
	}
	keys := tx.Message.AccountKeys
	if len(keys) == 0 {
		log.Printf("[tx_audit] %s: no account keys", contextLabel)
		return
	}
	log.Printf("[tx_audit] %s: fee_payer=%s num_signatures=%d num_account_keys=%d",
		contextLabel, keys[0].String(), len(tx.Signatures), len(keys))

	for ii, ix := range tx.Message.Instructions {
		if int(ix.ProgramIDIndex) >= len(keys) {
			continue
		}
		prog := keys[ix.ProgramIDIndex]
		if !prog.Equals(solana.TokenProgramID) {
			continue
		}
		if len(ix.Data) < 1 || ix.Data[0] != 3 { // spl-token Transfer
			continue
		}
		if len(ix.Accounts) < 3 {
			log.Printf("[tx_audit] %s: ix=%d token transfer but accounts<3", contextLabel, ii)
			continue
		}
		src := keys[ix.Accounts[0]]
		dst := keys[ix.Accounts[1]]
		auth := keys[ix.Accounts[2]]
		log.Printf("[tx_audit] %s: spl_transfer ix=%d source_ata=%s dest_ata=%s authority=%s",
			contextLabel, ii, src.String(), dst.String(), auth.String())
		return
	}
	log.Printf("[tx_audit] %s: no spl-token transfer (type 3) instruction found in legacy message", contextLabel)
}
