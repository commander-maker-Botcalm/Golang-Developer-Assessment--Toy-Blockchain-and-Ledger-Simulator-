package tests

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

func TestTransaction_String(t *testing.T) {
	tx := transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 10.5}
	s := tx.String()

	if !strings.Contains(s, "Alice") || !strings.Contains(s, "Bob") {
		t.Fatalf("Transaction.String() missing expected names: %s", s)
	}
	if !strings.Contains(s, "10.50000000") {
		t.Fatalf("Transaction.String() missing amount: %s", s)
	}
}

func TestAddTransaction_RejectsNegativeAmount(t *testing.T) {
	bc := blockchain.NewBlockchain()
	err := bc.AddTransaction(transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: -5})
	if err == nil {
		t.Fatal("expected error for negative transaction amount")
	}
}

func TestAddTransaction_RejectsZeroAmount(t *testing.T) {
	bc := blockchain.NewBlockchain()
	err := bc.AddTransaction(transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 0})
	if err == nil {
		t.Fatal("expected error for zero transaction amount")
	}
}

func TestAddTransaction_RejectsInsufficientBalance(t *testing.T) {
	bc := blockchain.NewBlockchain()
	err := bc.AddTransaction(transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 100})
	if err == nil {
		t.Fatal("expected error for insufficient balance")
	}
}

func TestAddTransaction_AllowsPendingFundsToBeSpent(t *testing.T) {
	bc := blockchain.NewBlockchain()

	// Give Alice initial funds
	if err := bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 20}); err != nil {
		t.Fatal(err)
	}

	// Generate a key pair for Alice
	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	publicKeyStr := transaction.PublicKeyToString(publicKey)

	// Alice sends 10 to Bob (this becomes pending)
	tx1 := transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Bob",
		Amount:    10,
		PublicKey: publicKeyStr,
	}
	sig1, err := transaction.SignTransaction(&tx1, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx1.Signature = sig1

	if err := bc.AddTransaction(tx1); err != nil {
		t.Fatal(err)
	}

	// Alice tries to send 15 to Carol (should fail because 20 - 10 pending = 10 remaining)
	tx2 := transaction.Transaction{
		Sender:    "Alice",
		Recipient: "Carol",
		Amount:    15,
		PublicKey: publicKeyStr,
	}
	sig2, err := transaction.SignTransaction(&tx2, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx2.Signature = sig2

	if err := bc.AddTransaction(tx2); err == nil {
		t.Fatal("expected insufficient balance error for Alice after pending spend")
	}
}
