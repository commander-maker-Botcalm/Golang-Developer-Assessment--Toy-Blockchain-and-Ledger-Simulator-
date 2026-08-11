package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"
	"toy-blockchain/transaction"
)

func newTestNode(t *testing.T, listenAddr string, peers []string, dataFile string, difficulty int) *Node {
	t.Helper()
	cfg := NodeConfig{ListenAddr: listenAddr, Peers: peers, DataFile: dataFile, Difficulty: difficulty, BlockSize: 10}
	n := NewNode(cfg)
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	return n
}

func waitForCondition(t *testing.T, deadline time.Time, fn func() bool) {
	t.Helper()
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestNodeAcceptsAndGossipsTransaction(t *testing.T) {
	tempDir := t.TempDir()
	aFile := filepath.Join(tempDir, "node-a.json")
	bFile := filepath.Join(tempDir, "node-b.json")

	a := newTestNode(t, "127.0.0.1:0", nil, aFile, 1)
	defer a.Shutdown(context.Background())
	b := newTestNode(t, "127.0.0.1:0", nil, bFile, 1)
	defer b.Shutdown(context.Background())

	a.Config.Peers = []string{fmt.Sprintf("http://%s", b.listener.Addr().String())}
	b.Config.Peers = []string{fmt.Sprintf("http://%s", a.listener.Addr().String())}

	// Make the funding available in confirmed chain state so the signed spend is valid on either node.
	if err := a.Blockchain.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatalf("failed to fund Alice on node A: %v", err)
	}
	a.Blockchain.MinePendingTransactions()
	if err := b.Blockchain.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatalf("failed to fund Alice on node B: %v", err)
	}
	b.Blockchain.MinePendingTransactions()
	waitForCondition(t, time.Now().Add(2*time.Second), func() bool {
		return len(a.Blockchain.Blocks) > 1 && len(b.Blockchain.Blocks) > 1
	})

	privateKey, err := transaction.GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	tx := transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 10}
	tx.PublicKey = transaction.PublicKeyToString(publicKey)
	sig, err := transaction.SignTransaction(&tx, privateKey)
	if err != nil {
		t.Fatalf("failed to sign transaction: %v", err)
	}
	tx.Signature = sig

	payload, err := json.Marshal(tx)
	if err != nil {
		t.Fatalf("failed to marshal transaction: %v", err)
	}

	resp, err := http.Post("http://"+a.listener.Addr().String()+"/transactions", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("failed to submit transaction: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}

	waitForCondition(t, time.Now().Add(2*time.Second), func() bool {
		return len(b.Blockchain.PendingTransactions) == 1
	})
	if len(b.Blockchain.PendingTransactions) != 1 {
		t.Fatalf("expected gossip to add one pending transaction, got %d", len(b.Blockchain.PendingTransactions))
	}
}

func TestNodeRejectsInvalidTransactionSignature(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "node.json")
	n := newTestNode(t, "127.0.0.1:0", nil, dataFile, 1)
	defer n.Shutdown(context.Background())

	tx := transaction.Transaction{Sender: "Alice", Recipient: "Bob", Amount: 10, PublicKey: "bad", Signature: "bad"}
	payload, _ := json.Marshal(tx)
	resp, err := http.Post("http://"+n.listener.Addr().String()+"/transactions", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("failed to submit transaction: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
	if len(n.Blockchain.PendingTransactions) != 0 {
		t.Fatalf("expected no pending transaction, got %d", len(n.Blockchain.PendingTransactions))
	}
}

func TestNodeAcceptsAndPropagatesBlock(t *testing.T) {
	tempDir := t.TempDir()
	aFile := filepath.Join(tempDir, "node-a.json")
	bFile := filepath.Join(tempDir, "node-b.json")

	a := newTestNode(t, "127.0.0.1:0", nil, aFile, 1)
	defer a.Shutdown(context.Background())
	b := newTestNode(t, "127.0.0.1:0", nil, bFile, 1)
	defer b.Shutdown(context.Background())

	a.Config.Peers = []string{fmt.Sprintf("http://%s", b.listener.Addr().String())}
	b.Config.Peers = []string{fmt.Sprintf("http://%s", a.listener.Addr().String())}

	if err := a.Blockchain.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatalf("failed to add funding transaction: %v", err)
	}
	if err := a.Blockchain.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Bob", Amount: 50}); err != nil {
		t.Fatalf("failed to add second funding transaction: %v", err)
	}
	a.Blockchain.MinePendingTransactions()
	block := a.Blockchain.Blocks[len(a.Blockchain.Blocks)-1]
	payload, _ := json.Marshal(block)

	resp, err := http.Post("http://"+b.listener.Addr().String()+"/blocks", "application/json", bytes.NewBuffer(payload))
	if err != nil {
		t.Fatalf("failed to submit block: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected status %d, got %d", http.StatusAccepted, resp.StatusCode)
	}
	waitForCondition(t, time.Now().Add(2*time.Second), func() bool {
		return len(b.Blockchain.Blocks) > 1
	})
	if len(b.Blockchain.Blocks) <= 1 {
		t.Fatalf("expected block to be appended, got %d blocks", len(b.Blockchain.Blocks))
	}
}

func TestThreeNodeBlockBroadcast(t *testing.T) {
	tempDir := t.TempDir()
	aFile := filepath.Join(tempDir, "node-a.json")
	bFile := filepath.Join(tempDir, "node-b.json")
	cFile := filepath.Join(tempDir, "node-c.json")

	a := newTestNode(t, "127.0.0.1:0", nil, aFile, 1)
	defer a.Shutdown(context.Background())
	b := newTestNode(t, "127.0.0.1:0", nil, bFile, 1)
	defer b.Shutdown(context.Background())
	c := newTestNode(t, "127.0.0.1:0", nil, cFile, 1)
	defer c.Shutdown(context.Background())

	a.Config.Peers = []string{
		fmt.Sprintf("http://%s", b.listener.Addr().String()),
		fmt.Sprintf("http://%s", c.listener.Addr().String()),
	}
	b.Config.Peers = []string{
		fmt.Sprintf("http://%s", a.listener.Addr().String()),
		fmt.Sprintf("http://%s", c.listener.Addr().String()),
	}
	c.Config.Peers = []string{
		fmt.Sprintf("http://%s", a.listener.Addr().String()),
		fmt.Sprintf("http://%s", b.listener.Addr().String()),
	}

	if err := a.Blockchain.AddTransaction(transaction.Transaction{Sender: "SYSTEM", Recipient: "Alice", Amount: 100}); err != nil {
		t.Fatalf("failed to add funding transaction: %v", err)
	}
	a.Blockchain.MinePendingTransactions()
	block := a.Blockchain.Blocks[len(a.Blockchain.Blocks)-1]

	a.BroadcastBlock(block)

	waitForCondition(t, time.Now().Add(2*time.Second), func() bool {
		return len(b.Blockchain.Blocks) > 1 && len(c.Blockchain.Blocks) > 1
	})

	if len(b.Blockchain.Blocks) <= 1 {
		t.Fatalf("expected node B to receive block, got %d blocks", len(b.Blockchain.Blocks))
	}
	if len(c.Blockchain.Blocks) <= 1 {
		t.Fatalf("expected node C to receive block, got %d blocks", len(c.Blockchain.Blocks))
	}
}
