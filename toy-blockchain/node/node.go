package node

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"toy-blockchain/blockchain"
	"toy-blockchain/transaction"
)

const gossipTimeout = 2 * time.Second

type NodeConfig struct {
	ListenAddr string
	Peers      []string
	DataFile   string
	Difficulty int
	BlockSize  int
}

type Node struct {
	Config     NodeConfig
	Blockchain *blockchain.Blockchain
	httpServer *http.Server
	listener   net.Listener
	lock       sync.RWMutex

	seenTransactions map[string]struct{}
	seenBlocks       map[string]struct{}

	// Fork tracking: stores alternative blocks that create competing chains
	// Key: block index, Value: map of hash -> block
	// Allows us to track competing blocks at the same height
	forks map[int]map[string]*blockchain.Block

	logger *log.Logger
}

func NewNode(config NodeConfig) *Node {
	return &Node{
		Config:           config,
		seenTransactions: make(map[string]struct{}),
		seenBlocks:       make(map[string]struct{}),
		forks:            make(map[int]map[string]*blockchain.Block),
		logger:           log.New(os.Stderr, fmt.Sprintf("[node %s] ", config.ListenAddr), log.LstdFlags),
	}
}

func (n *Node) Start() error {
	if n.Blockchain == nil {
		bc, err := blockchain.LoadFromFile(n.Config.DataFile)
		if err != nil {
			return err
		}
		if n.Config.Difficulty > 0 {
			bc.Difficulty = n.Config.Difficulty
		}
		if n.Config.BlockSize > 0 {
			bc.BlockSize = n.Config.BlockSize
		}
		n.Blockchain = bc
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", n.healthHandler)
	mux.HandleFunc("/status", n.statusHandler)
	mux.HandleFunc("/peers", n.peersHandler)
	mux.HandleFunc("/chain", n.chainHandler)
	mux.HandleFunc("/chain/info", n.chainInfoHandler)
	mux.HandleFunc("/block/", n.blockHandler)
	mux.HandleFunc("/mempool", n.mempoolHandler)
	mux.HandleFunc("/balances", n.balancesHandler)
	mux.HandleFunc("/transactions", n.transactionsHandler)
	mux.HandleFunc("/blocks", n.blocksHandler)
	mux.HandleFunc("/blocks/range", n.blocksRangeHandler)
	mux.HandleFunc("/sync", n.syncHandler)

	ln, err := net.Listen("tcp", n.Config.ListenAddr)
	if err != nil {
		return err
	}

	n.httpServer = &http.Server{Addr: ln.Addr().String(), Handler: mux}
	n.listener = ln

	go func() {
		_ = n.httpServer.Serve(ln)
	}()

	return nil
}

func (n *Node) Shutdown(ctx context.Context) error {
	if n.httpServer == nil {
		return nil
	}
	return n.httpServer.Shutdown(ctx)
}

// Addr returns the actual listening address for the node.
func (n *Node) Addr() string {
	if n.listener != nil {
		return n.listener.Addr().String()
	}
	return n.Config.ListenAddr
}

func (n *Node) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "OK")
}

func (n *Node) statusHandler(w http.ResponseWriter, r *http.Request) {
	n.lock.RLock()
	defer n.lock.RUnlock()
	if n.Blockchain == nil {
		writeJSON(w, map[string]interface{}{"peers": n.Config.Peers, "data_file": n.Config.DataFile})
		return
	}
	status := map[string]interface{}{
		"peers":        append([]string(nil), n.Config.Peers...),
		"data_file":    n.Config.DataFile,
		"difficulty":   n.Blockchain.Difficulty,
		"block_size":   n.Blockchain.BlockSize,
		"chain_length": len(n.Blockchain.Blocks),
	}
	writeJSON(w, status)
}

func (n *Node) peersHandler(w http.ResponseWriter, r *http.Request) {
	n.lock.RLock()
	defer n.lock.RUnlock()
	writeJSON(w, append([]string(nil), n.Config.Peers...))
}

func (n *Node) chainHandler(w http.ResponseWriter, r *http.Request) {
	n.lock.RLock()
	defer n.lock.RUnlock()
	writeJSON(w, n.Blockchain.Blocks)
}

func (n *Node) blockHandler(w http.ResponseWriter, r *http.Request) {
	prefix := "/block/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	idxStr := strings.TrimPrefix(r.URL.Path, prefix)
	if idxStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, "block index required")
		return
	}
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "invalid block index: %v", err)
		return
	}
	n.lock.RLock()
	defer n.lock.RUnlock()
	if n.Blockchain == nil || idx < 0 || idx >= len(n.Blockchain.Blocks) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "block not found")
		return
	}
	writeJSON(w, n.Blockchain.Blocks[idx])
}

func (n *Node) mempoolHandler(w http.ResponseWriter, r *http.Request) {
	n.lock.RLock()
	defer n.lock.RUnlock()
	writeJSON(w, n.Blockchain.PendingTransactions)
}

func (n *Node) balancesHandler(w http.ResponseWriter, r *http.Request) {
	n.lock.RLock()
	defer n.lock.RUnlock()
	writeJSON(w, n.Blockchain.GetBalances())
}

func (n *Node) transactionsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var tx transaction.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		http.Error(w, fmt.Sprintf("invalid transaction JSON: %v", err), http.StatusBadRequest)
		return
	}
	accepted, err := n.handleIncomingTransaction(tx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !accepted {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"duplicate"}`)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
}

func (n *Node) blocksHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var block blockchain.Block
	if err := json.NewDecoder(r.Body).Decode(&block); err != nil {
		http.Error(w, fmt.Sprintf("invalid block JSON: %v", err), http.StatusBadRequest)
		return
	}
	accepted, err := n.handleIncomingBlock(&block)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if !accepted {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"duplicate"}`)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprint(w, `{"status":"accepted"}`)
}

func (n *Node) handleIncomingTransaction(tx transaction.Transaction) (bool, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.Blockchain == nil {
		return false, fmt.Errorf("blockchain is not initialized")
	}
	txID := transaction.TransactionID(tx)
	if _, ok := n.seenTransactions[txID]; ok {
		n.logger.Printf("duplicate transaction ignored tx=%s", txID)
		return false, nil
	}
	if err := n.Blockchain.AddTransaction(tx); err != nil {
		n.logger.Printf("rejected transaction tx=%s err=%v", txID, err)
		return false, err
	}
	n.seenTransactions[txID] = struct{}{}
	if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
		n.logger.Printf("failed to persist blockchain after accepted transaction tx=%s err=%v", txID, err)
	}
	n.logger.Printf("accepted transaction tx=%s", txID)
	go n.gossipTransaction(tx)
	return true, nil
}

func (n *Node) handleIncomingBlock(block *blockchain.Block) (bool, error) {
	n.lock.Lock()
	defer n.lock.Unlock()
	if n.Blockchain == nil {
		return false, fmt.Errorf("blockchain is not initialized")
	}
	blockID := block.Hash
	if _, ok := n.seenBlocks[blockID]; ok {
		n.logger.Printf("duplicate block ignored hash=%s", blockID)
		return false, nil
	}
	if err := n.Blockchain.ValidateBlock(block); err != nil {
		n.logger.Printf("rejected block hash=%s err=%v", blockID, err)
		return false, err
	}

	// Check if this block extends the current chain
	currentHead := n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1]
	isLinear := (block.PrevHash == currentHead.Hash && block.Index == currentHead.Index+1)

	if isLinear {
		// Normal case: block extends current chain
		n.Blockchain.Blocks = append(n.Blockchain.Blocks, block)
		n.Blockchain.RemovePendingTransactions(block.Transactions)
		n.seenBlocks[blockID] = struct{}{}
		n.logger.Printf("accepted block hash=%s (extends chain)", blockID)
		if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
			n.logger.Printf("failed to persist blockchain after accepted block hash=%s err=%v", blockID, err)
		}
		go n.gossipBlock(block)
		return true, nil
	} else {
		// Fork case: block is valid but doesn't extend current chain
		// Store it as a competing block for potential later reorganization
		if n.forks[block.Index] == nil {
			n.forks[block.Index] = make(map[string]*blockchain.Block)
		}
		n.forks[block.Index][blockID] = block
		n.seenBlocks[blockID] = struct{}{}
		n.logger.Printf("stored competing block hash=%s at index=%d (potential fork)", blockID, block.Index)
		return false, nil // Not added to main chain but marked as seen
	}
}

func (n *Node) BroadcastBlock(block *blockchain.Block) {
	n.lock.Lock()
	if _, ok := n.seenBlocks[block.Hash]; ok {
		n.lock.Unlock()
		return
	}
	n.seenBlocks[block.Hash] = struct{}{}
	n.lock.Unlock()
	n.gossipBlock(block)
}

func (n *Node) BroadcastTransaction(tx transaction.Transaction) {
	txID := transaction.TransactionID(tx)
	n.lock.Lock()
	if _, ok := n.seenTransactions[txID]; ok {
		n.lock.Unlock()
		return
	}
	n.seenTransactions[txID] = struct{}{}
	n.lock.Unlock()
	n.gossipTransaction(tx)
}

func (n *Node) gossipTransaction(tx transaction.Transaction) {
	peers := n.copyPeers()
	if len(peers) == 0 {
		return
	}
	txID := transaction.TransactionID(tx)
	payload, err := json.Marshal(tx)
	if err != nil {
		n.logger.Printf("failed to encode transaction gossip tx=%s err=%v", txID, err)
		return
	}
	for _, peer := range peers {
		client := &http.Client{Timeout: gossipTimeout}
		url := strings.TrimRight(peer, "/") + "/transactions"
		resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
		if err != nil {
			n.logger.Printf("gossip transaction tx=%s peer=%s err=%v", txID, peer, err)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		if resp != nil && resp.StatusCode >= 400 {
			n.logger.Printf("gossip transaction tx=%s peer=%s status=%d", txID, peer, resp.StatusCode)
		}
	}
}

func (n *Node) gossipBlock(block *blockchain.Block) {
	peers := n.copyPeers()
	if len(peers) == 0 {
		return
	}
	payload, err := json.Marshal(block)
	if err != nil {
		n.logger.Printf("failed to encode block gossip hash=%s err=%v", block.Hash, err)
		return
	}
	for _, peer := range peers {
		client := &http.Client{Timeout: gossipTimeout}
		url := strings.TrimRight(peer, "/") + "/blocks"
		resp, err := client.Post(url, "application/json", bytes.NewBuffer(payload))
		if err != nil {
			n.logger.Printf("gossip block hash=%s peer=%s err=%v", block.Hash, peer, err)
			continue
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		if resp != nil && resp.StatusCode >= 400 {
			n.logger.Printf("gossip block hash=%s peer=%s status=%d", block.Hash, peer, resp.StatusCode)
		}
	}
}

func (n *Node) copyPeers() []string {
	n.lock.RLock()
	defer n.lock.RUnlock()
	return append([]string(nil), n.Config.Peers...)
}

// GetCompetingBlocks returns all blocks stored as forks, grouped by height.
// This is used for fork resolution to find the longest valid alternative chain.
func (n *Node) GetCompetingBlocks() map[int]map[string]*blockchain.Block {
	n.lock.RLock()
	defer n.lock.RUnlock()

	// Return a deep copy to avoid external modification
	result := make(map[int]map[string]*blockchain.Block)
	for idx, blocks := range n.forks {
		result[idx] = make(map[string]*blockchain.Block)
		for hash, block := range blocks {
			result[idx][hash] = block
		}
	}
	return result
}

// BuildCandidateChain attempts to build a chain starting from the given fork block.
// It traverses forward using available fork blocks to build the longest possible
// competing chain. Returns nil if no chain can be built.
func (n *Node) BuildCandidateChain(startBlock *blockchain.Block, competingBlocks map[int]map[string]*blockchain.Block) []*blockchain.Block {
	if startBlock == nil {
		return nil
	}

	chain := []*blockchain.Block{startBlock}
	currentBlock := startBlock

	// Try to extend the chain by finding blocks at the next indices
	for nextIdx := currentBlock.Index + 1; nextIdx < 1000; nextIdx++ { // Safety limit
		candidates, ok := competingBlocks[nextIdx]
		if !ok || len(candidates) == 0 {
			// No more blocks available at this height
			break
		}

		// Find a block that links to the current block
		found := false
		for _, candidate := range candidates {
			if candidate.PrevHash == currentBlock.Hash {
				chain = append(chain, candidate)
				currentBlock = candidate
				found = true
				break
			}
		}

		if !found {
			// Can't continue the chain
			break
		}
	}

	return chain
}

// CompareChains returns true if candidateChain is longer than currentChain.
// Both chains must be valid before calling this function.
func (n *Node) CompareChains(currentChain, candidateChain []*blockchain.Block) bool {
	if candidateChain == nil {
		return false
	}
	if currentChain == nil {
		return len(candidateChain) > 0
	}
	return len(candidateChain) > len(currentChain)
}

// ClearForksAt removes all fork entries at or below a given index.
// This is used after chain reorganization to clean up obsolete forks.
func (n *Node) ClearForksAt(upToIndex int) {
	n.lock.Lock()
	defer n.lock.Unlock()

	for idx := 0; idx <= upToIndex; idx++ {
		delete(n.forks, idx)
	}
}

// chainInfoHandler handles GET /chain/info
// Returns: {"height": <num_blocks>, "head_hash": "..."}
func (n *Node) chainInfoHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	n.lock.RLock()
	defer n.lock.RUnlock()
	if n.Blockchain == nil || len(n.Blockchain.Blocks) == 0 {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"blockchain not initialized"}`)
		return
	}
	height := len(n.Blockchain.Blocks) - 1
	headHash := n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1].Hash
	writeJSON(w, map[string]interface{}{
		"height":    height,
		"head_hash": headHash,
	})
}

// blocksRangeHandler handles GET /blocks/range?from=X&to=Y
// Returns a JSON array of blocks from index X to Y (inclusive).
func (n *Node) blocksRangeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	if fromStr == "" || toStr == "" {
		http.Error(w, "missing from or to query parameter", http.StatusBadRequest)
		return
	}
	from, err := strconv.Atoi(fromStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid from: %v", err), http.StatusBadRequest)
		return
	}
	to, err := strconv.Atoi(toStr)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid to: %v", err), http.StatusBadRequest)
		return
	}
	n.lock.RLock()
	defer n.lock.RUnlock()
	if n.Blockchain == nil {
		http.Error(w, "blockchain not initialized", http.StatusInternalServerError)
		return
	}
	if from < 0 || from >= len(n.Blockchain.Blocks) {
		http.Error(w, fmt.Sprintf("from index %d out of range", from), http.StatusBadRequest)
		return
	}
	if to < from || to >= len(n.Blockchain.Blocks) {
		http.Error(w, fmt.Sprintf("to index %d out of range", to), http.StatusBadRequest)
		return
	}
	blocks := n.Blockchain.Blocks[from : to+1]
	writeJSON(w, blocks)
}

// syncHandler handles GET /sync?peer=<peerURL> and triggers manual sync.
func (n *Node) syncHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	peerURL := r.URL.Query().Get("peer")
	if peerURL == "" {
		n.lock.RLock()
		if len(n.Config.Peers) > 0 {
			peerURL = n.Config.Peers[0]
		}
		n.lock.RUnlock()
	}
	if peerURL == "" {
		http.Error(w, "missing peer query parameter", http.StatusBadRequest)
		return
	}
	if !strings.HasPrefix(peerURL, "http://") && !strings.HasPrefix(peerURL, "https://") {
		peerURL = "http://" + peerURL
	}

	err := n.SyncFromPeer(peerURL)
	if err != nil {
		http.Error(w, fmt.Sprintf("sync failed: %v", err), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]interface{}{"status": "synced", "peer": peerURL})
}

// SyncFromPeer attempts to synchronize the local blockchain with a peer.
// It fetches the peer's chain height, compares it with the local chain,
// and if the peer has a longer valid chain, downloads and validates missing blocks.
func (n *Node) SyncFromPeer(peerURL string) error {
	peerURL = strings.TrimRight(peerURL, "/")

	// Step 1: Get peer's chain info
	n.logger.Printf("sync: fetching chain info from %s", peerURL)
	peerInfo, err := n.fetchChainInfo(peerURL)
	if err != nil {
		return fmt.Errorf("failed to fetch peer chain info: %w", err)
	}

	// Extract height from map
	peerHeightVal, ok := peerInfo["height"]
	if !ok {
		return fmt.Errorf("peer response missing height field")
	}
	peerHeight, ok := peerHeightVal.(float64)
	if !ok {
		return fmt.Errorf("peer height is not a number")
	}

	// Step 2: Check if peer has a longer chain
	n.lock.RLock()
	localHeight := len(n.Blockchain.Blocks) - 1
	n.lock.RUnlock()

	peerHeightInt := int(peerHeight)
	if peerHeightInt <= localHeight {
		n.logger.Printf("sync: peer height %d <= local height %d, no sync needed", peerHeightInt, localHeight)
		return nil
	}

	n.logger.Printf("sync: peer height %d > local height %d, attempting sync", peerHeightInt, localHeight)

	// Step 3: Download missing blocks from peer
	startIdx := localHeight + 1
	endIdx := peerHeightInt
	blocks, err := n.fetchBlockRange(peerURL, startIdx, endIdx)
	if err != nil {
		return fmt.Errorf("failed to fetch blocks from peer: %w", err)
	}

	if len(blocks) == 0 {
		return fmt.Errorf("peer returned no blocks for range %d-%d", startIdx, endIdx)
	}

	// Step 4: Validate and apply blocks
	n.logger.Printf("sync: validating %d blocks", len(blocks))
	return n.ApplySyncedBlocks(blocks)
}

// fetchChainInfo retrieves chain info from a peer.
func (n *Node) fetchChainInfo(peerURL string) (map[string]interface{}, error) {
	url := peerURL + "/chain/info"
	client := &http.Client{Timeout: gossipTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	var info map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return info, nil
}

// fetchBlockRange retrieves a range of blocks from a peer.
func (n *Node) fetchBlockRange(peerURL string, from, to int) ([]*blockchain.Block, error) {
	url := fmt.Sprintf("%s/blocks/range?from=%d&to=%d", peerURL, from, to)
	client := &http.Client{Timeout: gossipTimeout}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer returned status %d", resp.StatusCode)
	}

	var blocks []*blockchain.Block
	if err := json.NewDecoder(resp.Body).Decode(&blocks); err != nil {
		return nil, err
	}
	return blocks, nil
}

// ApplySyncedBlocks validates and appends downloaded blocks to the local chain.
// It validates each block sequentially against the current chain state,
// updating the chain tip as each block is successfully validated.
// If validation fails, the chain is left in the state before the failed block.
func (n *Node) ApplySyncedBlocks(blocks []*blockchain.Block) error {
	if len(blocks) == 0 {
		return nil
	}

	n.lock.Lock()
	defer n.lock.Unlock()

	if n.Blockchain == nil || len(n.Blockchain.Blocks) == 0 {
		return fmt.Errorf("blockchain not initialized")
	}

	// Step: Validate and apply blocks one by one
	// Each block is validated against the current tip, then appended to the chain
	for i, block := range blocks {
		if block == nil {
			return fmt.Errorf("block %d is nil", i)
		}

		blockID := block.Hash

		// Skip if already seen
		if _, ok := n.seenBlocks[blockID]; ok {
			n.logger.Printf("sync: block already seen, skipping hash=%s", blockID)
			continue
		}

		// Validate block against current chain state
		if err := n.Blockchain.ValidateBlock(block); err != nil {
			return fmt.Errorf("validation failed at block index %d: %w", i, err)
		}

		// Append block and update state
		n.Blockchain.Blocks = append(n.Blockchain.Blocks, block)
		n.Blockchain.RemovePendingTransactions(block.Transactions)
		n.seenBlocks[blockID] = struct{}{}
		n.logger.Printf("sync: applied block index=%d hash=%s", block.Index, blockID)
	}

	// Persist to disk
	if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
		n.logger.Printf("sync: failed to persist blockchain after sync err=%v", err)
		return fmt.Errorf("failed to persist blockchain: %w", err)
	}

	n.logger.Printf("sync: successfully synced %d blocks", len(blocks))
	return nil
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
