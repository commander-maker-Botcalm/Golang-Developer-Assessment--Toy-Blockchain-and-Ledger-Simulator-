package tests

import (
	"crypto/ed25519"
	"testing"
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

func TestGenerateKeyPair(t *testing.T) {
	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("expected no error generating key pair, got %v", err)
	}
	if privateKey == nil {
		t.Fatal("expected non-nil private key")
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	if len(publicKey) != ed25519.PublicKeySize {
		t.Fatalf("expected valid public key size, got %d", len(publicKey))
	}
}

func TestSignAndVerifyTransaction(t *testing.T) {
	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyStr := transaction.PublicKeyToString(publicKey)

	tx := &transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10.5,
		PublicKey: publicKeyStr,
	}

	signature, err := transaction.SignTransaction(tx, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	if signature == "" {
		t.Fatal("expected non-empty signature")
	}

	tx.Signature = signature
	valid, err := transaction.VerifyTransaction(tx)
	if err != nil {
		t.Fatalf("failed to verify transaction: %v", err)
	}
	if !valid {
		t.Fatal("expected valid signature, but verification failed")
	}
}

func TestVerifyTransactionWithModifiedData(t *testing.T) {
	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyStr := transaction.PublicKeyToString(publicKey)

	tx := &transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10.5,
		PublicKey: publicKeyStr,
	}

	signature, err := transaction.SignTransaction(tx, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx.Signature = signature

	tx.Amount = 20.0

	valid, err := transaction.VerifyTransaction(tx)
	if err != nil {
		t.Fatalf("failed to verify transaction: %v", err)
	}
	if valid {
		t.Fatal("expected signature verification to fail after modifying transaction")
	}
}

func TestVerifyTransactionWithWrongKey(t *testing.T) {
	privateKey1, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair 1: %v", err)
	}

	privateKey2, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair 2: %v", err)
	}

	publicKey1 := privateKey1.Public().(ed25519.PublicKey)
	publicKeyStr1 := transaction.PublicKeyToString(publicKey1)

	tx := &transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10.5,
		PublicKey: publicKeyStr1,
	}

	signature, err := transaction.SignTransaction(tx, privateKey1)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx.Signature = signature

	publicKey2 := privateKey2.Public().(ed25519.PublicKey)
	tx.PublicKey = transaction.PublicKeyToString(publicKey2)

	valid, err := transaction.VerifyTransaction(tx)
	if err != nil {
		t.Fatalf("failed to verify transaction: %v", err)
	}
	if valid {
		t.Fatal("expected signature verification to fail with wrong public key")
	}
}

func TestAddTransactionWithValidSignature(t *testing.T) {
	bc := blockchain.NewBlockchain()

	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100})

	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyStr := transaction.PublicKeyToString(publicKey)

	tx := transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10.5,
		PublicKey: publicKeyStr,
	}

	signature, err := transaction.SignTransaction(&tx, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx.Signature = signature

	err = bc.AddTransaction(tx)
	if err != nil {
		t.Fatalf("expected transaction to be accepted, got error: %v", err)
	}

	if len(bc.PendingTransactions) != 2 {
		t.Fatalf("expected 2 pending transactions (1 SYSTEM + 1 signed), got %d", len(bc.PendingTransactions))
	}
}

func TestAddTransactionWithInvalidSignature(t *testing.T) {
	bc := blockchain.NewBlockchain()

	bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100})

	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyStr := transaction.PublicKeyToString(publicKey)

	tx := transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10.5,
		PublicKey: publicKeyStr,
		Signature: "invalid_signature_data",
	}

	err = bc.AddTransaction(tx)
	if err == nil {
		t.Fatal("expected transaction to be rejected due to invalid signature")
	}

	if len(bc.PendingTransactions) != 1 {
		t.Fatalf("expected 1 pending transaction (only SYSTEM), got %d", len(bc.PendingTransactions))
	}
}
