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
	logger           *log.Logger
}

func NewNode(config NodeConfig) *Node {
	return &Node{
		Config:           config,
		seenTransactions: make(map[string]struct{}),
		seenBlocks:       make(map[string]struct{}),
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
	mux.HandleFunc("/block/", n.blockHandler)
	mux.HandleFunc("/mempool", n.mempoolHandler)
	mux.HandleFunc("/balances", n.balancesHandler)
	mux.HandleFunc("/transactions", n.transactionsHandler)
	mux.HandleFunc("/blocks", n.blocksHandler)

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
	n.Blockchain.Blocks = append(n.Blockchain.Blocks, block)
	n.Blockchain.RemovePendingTransactions(block.Transactions)
	if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
		n.logger.Printf("failed to persist blockchain after accepted block hash=%s err=%v", blockID, err)
	}
	n.seenBlocks[blockID] = struct{}{}
	n.logger.Printf("accepted block hash=%s", blockID)
	go n.gossipBlock(block)
	return true, nil
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

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
