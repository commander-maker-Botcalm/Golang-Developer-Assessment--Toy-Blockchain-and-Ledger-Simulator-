package tests

import (
	"testing"
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

func TestCalculateHash_NotEmpty(t *testing.T) {
	b := makeBlock(0, nil, "", 1_000_000)
	if b.Hash == "" {
		t.Fatal("expected a non-empty hash, got empty string")
	}
}

func TestCalculateHash_Length(t *testing.T) {
	b := makeBlock(0, nil, "", 1_000_000)
	if len(b.Hash) != 64 {
		t.Fatalf("expected hash length 64, got %d", len(b.Hash))
	}
}

func TestCalculateHash_Deterministic(t *testing.T) {
	txs := singleTx("Alice", "Bob", 10.0)
	b1 := makeBlock(1, txs, "prevABC", 9_999_999)
	b2 := makeBlock(1, txs, "prevABC", 9_999_999)

	if b1.Hash != b2.Hash {
		t.Fatalf("hashing is not deterministic:\n  got  %s\n  want %s", b2.Hash, b1.Hash)
	}
}

func TestCalculateHash_DifferentData(t *testing.T) {
	b1 := makeBlock(1, singleTx("Alice", "Bob", 10.0), "prevXXX", 1_234_567)
	b2 := makeBlock(1, singleTx("Alice", "Bob", 99.0), "prevXXX", 1_234_567)

	if b1.Hash == b2.Hash {
		t.Fatal("different transactions produced the same hash")
	}
}

func TestCalculateHash_DifferentPrevHash(t *testing.T) {
	txs := singleTx("Carol", "Dave", 5.0)
	b1 := makeBlock(2, txs, "hash-AAA", 5_555_555)
	b2 := makeBlock(2, txs, "hash-BBB", 5_555_555)

	if b1.Hash == b2.Hash {
		t.Fatal("different PrevHash produced the same hash — chain integrity broken")
	}
}

func TestCalculateHash_GenesisHasNoPrevHash(t *testing.T) {
	genesis := makeBlock(0, []transaction.Transaction{}, "", 0)
	if genesis.Hash == "" {
		t.Fatal("Genesis block should still produce a valid hash")
	}
}

func TestCalculateHash_NonceAffectsHash(t *testing.T) {
	txs := singleTx("Alice", "Bob", 1.0)

	b1 := &blockchain.Block{Index: 1, Timestamp: 111, Transactions: txs, PrevHash: "abc", Nonce: 0}
	b1.Hash = blockchain.CalculateHash(b1)

	b2 := &blockchain.Block{Index: 1, Timestamp: 111, Transactions: txs, PrevHash: "abc", Nonce: 1}
	b2.Hash = blockchain.CalculateHash(b2)

	if b1.Hash == b2.Hash {
		t.Fatal("different Nonce values produced the same hash — PoW would be broken")
	}
}

func TestCalculateHash_MultipleTransactions(t *testing.T) {
	oneTx := []transaction.Transaction{{Sender: "Alice", Recipient: "Bob", Amount: 10.0}}
	twoTx := []transaction.Transaction{
		{Sender: "Alice", Recipient: "Bob", Amount: 10.0},
		{Sender: "Bob", Recipient: "Carol", Amount: 5.0},
	}

	b1 := makeBlock(1, oneTx, "prev", 777)
	b2 := makeBlock(1, twoTx, "prev", 777)

	if b1.Hash == b2.Hash {
		t.Fatal("blocks with different transaction counts produced the same hash")
	}
}

func TestHashInput_IndexIsIncluded(t *testing.T) {
	base := baseBlock()

	other := *base
	other.Index = 999
	other.Hash = blockchain.CalculateHash(&other)

	if base.Hash == other.Hash {
		t.Fatal("changing Index did not change the hash — Index is missing from hash input")
	}
}

func TestHashInput_TimestampIsIncluded(t *testing.T) {
	base := baseBlock()

	other := *base
	other.Timestamp = 9_999_999
	other.Hash = blockchain.CalculateHash(&other)

	if base.Hash == other.Hash {
		t.Fatal("changing Timestamp did not change the hash — Timestamp is missing from hash input")
	}
}

func TestHashInput_TransactionsIsIncluded(t *testing.T) {
	base := baseBlock()

	other := *base
	other.Transactions = singleTx("Alice", "Bob", 99.99)
	other.Hash = blockchain.CalculateHash(&other)

	if base.Hash == other.Hash {
		t.Fatal("changing Transactions did not change the hash — Transactions missing from hash input")
	}
}

func TestHashInput_PrevHashIsIncluded(t *testing.T) {
	base := baseBlock()

	other := *base
	other.PrevHash = "0000000000000000000000000000000000000000000000000000000000000000"
	other.Hash = blockchain.CalculateHash(&other)

	if base.Hash == other.Hash {
		t.Fatal("changing PrevHash did not change the hash — PrevHash missing from hash input")
	}
}

func TestHashInput_NonceIsIncluded(t *testing.T) {
	base := baseBlock()

	other := *base
	other.Nonce = 42
	other.Hash = blockchain.CalculateHash(&other)

	if base.Hash == other.Hash {
		t.Fatal("changing Nonce did not change the hash — Nonce missing from hash input")
	}
}

func TestHashInput_HashFieldIsExcluded(t *testing.T) {
	b := baseBlock()
	firstHash := b.Hash

	b.Hash = "THIS_SHOULD_NOT_AFFECT_THE_OUTPUT"
	secondHash := blockchain.CalculateHash(b)

	if firstHash != secondHash {
		t.Fatalf(
			"Hash field appears to be included in hash input (circular!):\n  before: %s\n  after:  %s",
			firstHash, secondHash,
		)
	}
}

func TestHashInput_FieldOrderMatters(t *testing.T) {
	txs := singleTx("Alice", "Bob", 10.0)

	a := &blockchain.Block{Index: 1, Timestamp: 2, Transactions: txs, PrevHash: "ph", Nonce: 3}
	a.Hash = blockchain.CalculateHash(a)

	b := &blockchain.Block{Index: 3, Timestamp: 2, Transactions: txs, PrevHash: "ph", Nonce: 1}
	b.Hash = blockchain.CalculateHash(b)

	if a.Hash == b.Hash {
		t.Fatal("swapping field values produced the same hash — field order is not enforced")
	}
}
