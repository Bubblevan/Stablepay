package blockchain

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/gagliardetto/solana-go/programs/token"
	"github.com/stablepay/blockchain-adapter/internal/domain/gateway"
)

func decodeAndValidateTransaction(base64Tx string, requireSignature bool) (*solana.Transaction, error) {
	if base64Tx == "" {
		return nil, fmt.Errorf("transaction is empty")
	}
	tx := &solana.Transaction{}
	if err := tx.UnmarshalBase64(base64Tx); err != nil {
		return nil, fmt.Errorf("invalid transaction format: %w", err)
	}
	raw, err := base64.StdEncoding.DecodeString(base64Tx)
	if err != nil {
		return nil, fmt.Errorf("invalid transaction encoding: %w", err)
	}
	canonical, err := tx.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to encode transaction: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return nil, fmt.Errorf("transaction contains trailing or non-canonical data")
	}
	message := &tx.Message
	required := int(message.Header.NumRequiredSignatures)
	if len(message.AccountKeys) == 0 {
		return nil, fmt.Errorf("transaction has no account keys")
	}
	if required == 0 || required > len(message.AccountKeys) {
		return nil, fmt.Errorf("invalid required signature count %d", required)
	}
	if int(message.Header.NumReadonlySignedAccounts) > required {
		return nil, fmt.Errorf("readonly signed accounts exceed required signatures")
	}
	if int(message.Header.NumReadonlyUnsignedAccounts) > len(message.AccountKeys)-required {
		return nil, fmt.Errorf("readonly unsigned accounts exceed unsigned accounts")
	}
	if len(tx.Signatures) != required {
		return nil, fmt.Errorf("signature count %d does not match required signatures %d", len(tx.Signatures), required)
	}
	if message.RecentBlockhash == (solana.Hash{}) {
		return nil, fmt.Errorf("recent blockhash is empty")
	}
	if len(message.Instructions) == 0 {
		return nil, fmt.Errorf("transaction has no instructions")
	}
	if message.IsVersioned() && len(message.AddressTableLookups) > 0 {
		return nil, fmt.Errorf("versioned transactions with unresolved address lookups are not supported")
	}
	for instructionIndex, instruction := range message.Instructions {
		if int(instruction.ProgramIDIndex) >= len(message.AccountKeys) {
			return nil, fmt.Errorf("instruction %d has invalid program id index %d", instructionIndex, instruction.ProgramIDIndex)
		}
		for _, accountIndex := range instruction.Accounts {
			if int(accountIndex) >= len(message.AccountKeys) {
				return nil, fmt.Errorf("instruction %d has invalid account index %d", instructionIndex, accountIndex)
			}
		}
		if len(instruction.Data) == 0 {
			return nil, fmt.Errorf("instruction %d has empty data", instructionIndex)
		}
	}

	messageBytes, err := message.MarshalBinary()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize transaction message: %w", err)
	}
	validSignatures := 0
	for index, signature := range tx.Signatures {
		if signature == (solana.Signature{}) {
			continue
		}
		if !signature.Verify(message.AccountKeys[index], messageBytes) {
			return nil, fmt.Errorf("invalid signature for signer %s", message.AccountKeys[index].String())
		}
		validSignatures++
	}
	if requireSignature && validSignatures == 0 {
		return nil, fmt.Errorf("transaction has no valid signatures")
	}
	return tx, nil
}

func (s *SolanaGatewayImpl) ValidatePartiallySignedTransaction(base64Tx string) error {
	_, err := decodeAndValidateTransaction(base64Tx, true)
	return err
}

func (s *SolanaGatewayImpl) ValidateFeePayer(base64Tx, expectedFeePayer string) error {
	tx, err := decodeAndValidateTransaction(base64Tx, false)
	if err != nil {
		return err
	}
	if expectedFeePayer == "" {
		return fmt.Errorf("expected fee payer is empty")
	}
	expected, err := solana.PublicKeyFromBase58(expectedFeePayer)
	if err != nil {
		return fmt.Errorf("invalid expected fee payer: %w", err)
	}
	actual := tx.Message.AccountKeys[0]
	if !actual.Equals(expected) {
		return fmt.Errorf("fee payer mismatch: expected %s, got %s", expected, actual)
	}
	writable, err := tx.Message.IsWritable(actual)
	if err != nil {
		return fmt.Errorf("failed to inspect fee payer: %w", err)
	}
	if !writable {
		return fmt.Errorf("fee payer must be writable")
	}
	return nil
}

func (s *SolanaGatewayImpl) ValidateTransferTransaction(base64Tx, fromAddress, toAddress, currency string, amount uint64) error {
	tx, err := decodeAndValidateTransaction(base64Tx, true)
	if err != nil {
		return err
	}
	if err := s.ValidateFeePayer(base64Tx, s.GetFeePayerAddress()); err != nil {
		return err
	}
	from, err := solana.PublicKeyFromBase58(fromAddress)
	if err != nil {
		return fmt.Errorf("invalid from address: %w", err)
	}
	to, err := solana.PublicKeyFromBase58(toAddress)
	if err != nil {
		return fmt.Errorf("invalid to address: %w", err)
	}
	mintAddress := gateway.GetTokenMintByCurrency(currency, gateway.IsMainnet(s.network))
	if mintAddress == "" {
		return fmt.Errorf("unsupported currency: %s", currency)
	}
	mint, err := solana.PublicKeyFromBase58(mintAddress)
	if err != nil {
		return fmt.Errorf("invalid mint address: %w", err)
	}
	fromATA, _, err := solana.FindAssociatedTokenAddress(from, mint)
	if err != nil {
		return fmt.Errorf("failed to derive source ATA: %w", err)
	}
	toATA, _, err := solana.FindAssociatedTokenAddress(to, mint)
	if err != nil {
		return fmt.Errorf("failed to derive destination ATA: %w", err)
	}
	fromIndex := -1
	for index, key := range tx.Message.AccountKeys {
		if key.Equals(from) {
			fromIndex = index
			break
		}
	}
	if fromIndex < 0 || fromIndex >= int(tx.Message.Header.NumRequiredSignatures) {
		return fmt.Errorf("from address is not a transaction signer")
	}
	if tx.Signatures[fromIndex] == (solana.Signature{}) {
		return fmt.Errorf("from address signature is missing")
	}

	for instructionIndex, instruction := range tx.Message.Instructions {
		program, err := tx.Message.Program(instruction.ProgramIDIndex)
		if err != nil || !program.Equals(token.ProgramID) {
			continue
		}
		if len(instruction.Accounts) < 3 {
			return fmt.Errorf("token instruction %d has too few accounts", instructionIndex)
		}
		data := []byte(instruction.Data)
		if len(data) == 9 && data[0] == 3 {
			if !accountAt(tx, instruction.Accounts[0], fromATA) || !accountAt(tx, instruction.Accounts[1], toATA) || !accountAt(tx, instruction.Accounts[2], from) {
				return fmt.Errorf("token transfer instruction %d accounts do not match request", instructionIndex)
			}
			if binary.LittleEndian.Uint64(data[1:]) != amount {
				return fmt.Errorf("token transfer amount does not match request")
			}
			return nil
		}
		if len(data) == 10 && data[0] == 12 && len(instruction.Accounts) >= 4 {
			if !accountAt(tx, instruction.Accounts[0], fromATA) || !accountAt(tx, instruction.Accounts[2], toATA) || !accountAt(tx, instruction.Accounts[3], from) {
				return fmt.Errorf("checked token transfer instruction %d accounts do not match request", instructionIndex)
			}
			if binary.LittleEndian.Uint64(data[1:9]) != amount {
				return fmt.Errorf("checked token transfer amount does not match request")
			}
			return nil
		}
	}
	return fmt.Errorf("transaction does not contain a supported SPL token transfer")
}

func accountAt(tx *solana.Transaction, index uint16, expected solana.PublicKey) bool {
	account, err := tx.Message.Account(index)
	return err == nil && account.Equals(expected)
}
