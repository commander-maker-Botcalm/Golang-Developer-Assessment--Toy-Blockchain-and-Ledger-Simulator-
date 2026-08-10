package node

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"testing"
	"time"
)

type statusResponse struct {
	Peers      []string `json:"peers"`
	DataFile   string   `json:"data_file"`
	Difficulty int      `json:"difficulty"`
	BlockSize  int      `json:"block_size"`
	ChainLen   int      `json:"chain_length"`
}

func TestNodeHTTPHandlers(t *testing.T) {
	tempDir := t.TempDir()
	dataFile := filepath.Join(tempDir, "chain.json")
	config := NodeConfig{
		ListenAddr: "127.0.0.1:0",
		Peers:      []string{"http://localhost:9999"},
		DataFile:   dataFile,
		Difficulty: 5,
		BlockSize:  20,
	}

	n := NewNode(config)
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := n.Shutdown(ctx); err != nil {
			t.Fatalf("failed to shutdown node: %v", err)
		}
	}()

	baseURL := "http://" + n.listener.Addr().String()

	resp, err := http.Get(baseURL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read health response: %v", err)
	}
	if string(body) != "OK" {
		t.Fatalf("health returned %q, want OK", string(body))
	}

	resp, err = http.Get(baseURL + "/status")
	if err != nil {
		t.Fatalf("status request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var status statusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status response: %v", err)
	}
	if status.DataFile != dataFile {
		t.Fatalf("expected data_file %q, got %q", dataFile, status.DataFile)
	}
	if status.Difficulty != 5 {
		t.Fatalf("expected difficulty 5, got %d", status.Difficulty)
	}
	if status.BlockSize != 20 {
		t.Fatalf("expected block_size 20, got %d", status.BlockSize)
	}
	if len(status.Peers) != 1 || status.Peers[0] != "http://localhost:9999" {
		t.Fatalf("unexpected peers: %#v", status.Peers)
	}
	if status.ChainLen != 1 {
		t.Fatalf("expected chain_length 1, got %d", status.ChainLen)
	}

	resp, err = http.Get(baseURL + "/peers")
	if err != nil {
		t.Fatalf("peers request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("peers returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var peers []string
	if err := json.NewDecoder(resp.Body).Decode(&peers); err != nil {
		t.Fatalf("failed to decode peers response: %v", err)
	}
	if len(peers) != 1 || peers[0] != "http://localhost:9999" {
		t.Fatalf("unexpected peer list: %#v", peers)
	}

	resp, err = http.Get(baseURL + "/chain")
	if err != nil {
		t.Fatalf("chain request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chain returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var chain []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&chain); err != nil {
		t.Fatalf("failed to decode chain response: %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("expected chain length 1, got %d", len(chain))
	}

	resp, err = http.Get(baseURL + "/block/0")
	if err != nil {
		t.Fatalf("block request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("block returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var block map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&block); err != nil {
		t.Fatalf("failed to decode block response: %v", err)
	}
	if idx, ok := block["index"].(float64); !ok || int(idx) != 0 {
		t.Fatalf("unexpected block index: %#v", block["index"])
	}

	resp, err = http.Get(baseURL + "/mempool")
	if err != nil {
		t.Fatalf("mempool request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("mempool returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var mempool []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&mempool); err != nil {
		t.Fatalf("failed to decode mempool response: %v", err)
	}
	if len(mempool) != 0 {
		t.Fatalf("expected empty mempool, got %d entries", len(mempool))
	}

	resp, err = http.Get(baseURL + "/balances")
	if err != nil {
		t.Fatalf("balances request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("balances returned %d, want %d", resp.StatusCode, http.StatusOK)
	}
	var balances map[string]float64
	if err := json.NewDecoder(resp.Body).Decode(&balances); err != nil {
		t.Fatalf("failed to decode balances response: %v", err)
	}
	if len(balances) != 0 {
		t.Fatalf("expected zero balances for new chain, got %#v", balances)
	}
}
