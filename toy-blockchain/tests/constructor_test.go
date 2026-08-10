package tests

import (
	"testing"
	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

func TestNewBlockchain_ConstructorFlow(t *testing.T) {
	bc := blockchain.NewBlockchain()
	if bc == nil {
		t.Fatal("Step 1 failed: NewBlockchain() returned nil")
	}

	if len(bc.Blocks) != 1 {
		t.Fatalf("Step 2 failed: expected 1 block after construction, got %d", len(bc.Blocks))
	}

	genesis := bc.Blocks[0]
	if genesis.Hash == "" {
		t.Fatal("Step 3 failed: Genesis block hash is empty — CalculateHash was not called")
	}
	if len(genesis.Hash) != 64 {
		t.Fatalf("Step 3 failed: Genesis hash length want 64, got %d", len(genesis.Hash))
	}

	if genesis.Index != 0 {
		t.Errorf("Step 4 failed: stored block Index want 0, got %d", genesis.Index)
	}
	if genesis.PrevHash != blockchain.GenesisPrevHash {
		t.Errorf("Step 4 failed: stored block PrevHash want 64 zeros, got %s", genesis.PrevHash)
	}

	bc.AddBlock([]transaction.Transaction{{Sender: "Alice", Recipient: "Bob", Amount: 1.0}})
	if len(bc.Blocks) != 2 {
		t.Fatalf("Step 5 failed: chain not usable after construction — expected 2 blocks, got %d", len(bc.Blocks))
	}
}

func TestGenesisBlock_IsDeterministic(t *testing.T) {
	bc1 := blockchain.NewBlockchain()
	bc2 := blockchain.NewBlockchain()

	g1 := bc1.Blocks[0]
	g2 := bc2.Blocks[0]

	if g1.Hash != g2.Hash {
		t.Fatalf("Genesis hash is not deterministic:\n  chain1: %s\n  chain2: %s", g1.Hash, g2.Hash)
	}
}

func TestGenesisBlock_SpecValues(t *testing.T) {
	bc := blockchain.NewBlockchain()
	g := bc.Blocks[0]

	if g.Index != 0 {
		t.Errorf("Genesis Index: want 0, got %d", g.Index)
	}
	if g.Timestamp != 0 {
		t.Errorf("Genesis Timestamp: want 0, got %d", g.Timestamp)
	}
	if g.Nonce != 0 {
		t.Errorf("Genesis Nonce: want 0, got %d", g.Nonce)
	}
	if g.PrevHash != blockchain.GenesisPrevHash {
		t.Errorf("Genesis PrevHash: want 64 zeros, got %s", g.PrevHash)
	}
	if len(g.PrevHash) != 64 {
		t.Errorf("Genesis PrevHash length: want 64, got %d", len(g.PrevHash))
	}
	if len(g.Transactions) != 0 {
		t.Errorf("Genesis Transactions: want empty slice, got %d txs", len(g.Transactions))
	}
	if g.Hash == "" {
		t.Error("Genesis Hash must not be empty")
	}
}
