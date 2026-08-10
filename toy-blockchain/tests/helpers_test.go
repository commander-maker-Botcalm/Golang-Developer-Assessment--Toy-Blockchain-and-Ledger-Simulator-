package tests

import (
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

// makeBlock builds a Block with a fixed timestamp for reproducibility.
func makeBlock(index int, txs []transaction.Transaction, prevHash string, timestamp int64) *blockchain.Block {
	b := &blockchain.Block{
		Index:        index,
		Timestamp:    timestamp,
		Transactions: txs,
		PrevHash:     prevHash,
		Nonce:        0,
	}
	b.Hash = blockchain.CalculateHash(b)
	return b
}

// singleTx is a convenience helper for tests that need one transaction.
func singleTx(from, to string, amount float64) []transaction.Transaction {
	return []transaction.Transaction{{Sender: from, Recipient: to, Amount: amount}}
}

// baseBlock returns a fully populated reference block used as the baseline
// for serialization contract tests.
func baseBlock() *blockchain.Block {
	b := &blockchain.Block{
		Index:        1,
		Timestamp:    1_000_000,
		Transactions: singleTx("Alice", "Bob", 10.0),
		PrevHash:     "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		Nonce:        0,
		Hash:         "",
	}
	b.Hash = blockchain.CalculateHash(b)
	return b
}
