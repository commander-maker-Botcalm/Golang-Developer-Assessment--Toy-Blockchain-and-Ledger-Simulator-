package tests

import (
	"strings"
	"testing"
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

func TestMinePendingTransactions_CreatesBlockFromPendingPool(t *testing.T) {
	bc := blockchain.NewBlockchain()

	if err := bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 50}); err != nil {
		t.Fatal(err)
	}
	if err := bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Bob", Amount: 20}); err != nil {
		t.Fatal(err)
	}
	if err := bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Charlie", Amount: 10}); err != nil {
		t.Fatal(err)
	}

	bc.MinePendingTransactions()

	if len(bc.Blocks) != 2 {
		t.Fatalf("expected 2 blocks after mining pending transactions, got %d", len(bc.Blocks))
	}

	minedBlock := bc.Blocks[1]
	if len(minedBlock.Transactions) != 3 {
		t.Fatalf("expected mined block to contain 3 transactions, got %d", len(minedBlock.Transactions))
	}

	if len(bc.PendingTransactions) != 0 {
		t.Fatalf("expected pending pool to be cleared after mining, got %d pending transactions", len(bc.PendingTransactions))
	}
}

func TestBlock_Mine(t *testing.T) {
	txs := singleTx("Alice", "Bob", 10.0)
	b := &blockchain.Block{
		Index:        1,
		Timestamp:    1234567,
		Transactions: txs,
		PrevHash:     "prevHash",
		Nonce:        0,
	}

	difficulty := 3
	b.Mine(difficulty)

	expectedPrefix := strings.Repeat("0", difficulty)
	if !strings.HasPrefix(b.Hash, expectedPrefix) {
		t.Fatalf("expected mined block hash to start with %q, got %q", expectedPrefix, b.Hash)
	}

	recalculatedHash := blockchain.CalculateHash(b)
	if b.Hash != recalculatedHash {
		t.Fatalf("mined block hash %q does not match recalculated hash %q", b.Hash, recalculatedHash)
	}
}

func TestBlock_MineWithWorkers_ProducesSameValidHash(t *testing.T) {
	txs := singleTx("Alice", "Bob", 10.0)
	b1 := &blockchain.Block{
		Index:        1,
		Timestamp:    1234567,
		Transactions: txs,
		PrevHash:     "prevHash",
		Nonce:        0,
	}
	b2 := &blockchain.Block{
		Index:        1,
		Timestamp:    1234567,
		Transactions: txs,
		PrevHash:     "prevHash",
		Nonce:        0,
	}

	difficulty := 3
	b1.Mine(difficulty)
	b2.MineWithWorkers(difficulty, 4)

	expectedPrefix := strings.Repeat("0", difficulty)
	if !strings.HasPrefix(b2.Hash, expectedPrefix) {
		t.Fatalf("expected mined block hash to start with %q, got %q", expectedPrefix, b2.Hash)
	}

	recalculatedHash := blockchain.CalculateHash(b2)
	if b2.Hash != recalculatedHash {
		t.Fatalf("mined block hash %q does not match recalculated hash %q", b2.Hash, recalculatedHash)
	}

	if b1.Hash != b2.Hash {
		t.Fatalf("expected sequential and concurrent mining to produce same hash for identical inputs, got %q and %q", b1.Hash, b2.Hash)
	}
}

func TestMinePendingTransactions_UsesConfiguredBlockSize(t *testing.T) {
	bc := blockchain.NewBlockchain()
	bc.BlockSize = 2

	if err := bc.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatal(err)
	}

	for _, tx := range []transaction.Transaction{
		{Sender: "SYSTEM", Recipient: "Bob", Amount: 10},
		{Sender: "SYSTEM", Recipient: "Carol", Amount: 5},
		{Sender: "SYSTEM", Recipient: "David", Amount: 3},
		{Sender: "SYSTEM", Recipient: "Eve", Amount: 2},
	} {
		if err := bc.AddTransaction(tx); err != nil {
			t.Fatal(err)
		}
	}

	bc.MinePendingTransactions()

	if len(bc.Blocks) != 2 {
		t.Fatalf("expected 2 blocks after mining with block size 2, got %d", len(bc.Blocks))
	}

	minedBlock := bc.Blocks[1]
	if len(minedBlock.Transactions) != 2 {
		t.Fatalf("expected mined block to contain 2 transactions, got %d", len(minedBlock.Transactions))
	}

	if len(bc.PendingTransactions) != 3 {
		t.Fatalf("expected 3 pending transactions to remain after mining, got %d", len(bc.PendingTransactions))
	}
}
