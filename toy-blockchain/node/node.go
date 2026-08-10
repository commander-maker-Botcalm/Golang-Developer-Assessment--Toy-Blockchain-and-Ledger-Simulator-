package node

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"toy-blockchain/blockchain"
)

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
}

func NewNode(config NodeConfig) *Node {
	return &Node{Config: config}
}

func (n *Node) Start() error {
	if n.Blockchain == nil {
		bc, err := blockchain.LoadFromFile(n.Config.DataFile)
		if err != nil {
			return err
		}
		if n.Config.Difficulty >= 0 {
			bc.Difficulty = n.Config.Difficulty
		}
		if n.Config.BlockSize >= 0 {
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

	ln, err := net.Listen("tcp", n.Config.ListenAddr)
	if err != nil {
		return err
	}

	n.httpServer = &http.Server{
		Addr:    ln.Addr().String(),
		Handler: mux,
	}
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
	status := map[string]interface{}{
		"peers":        n.Config.Peers,
		"data_file":    n.Config.DataFile,
		"difficulty":   n.Blockchain.Difficulty,
		"block_size":   n.Blockchain.BlockSize,
		"chain_length": len(n.Blockchain.Blocks),
	}
	writeJSON(w, status)
}

func (n *Node) peersHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, n.Config.Peers)
}

func (n *Node) chainHandler(w http.ResponseWriter, r *http.Request) {
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
	if idx < 0 || idx >= len(n.Blockchain.Blocks) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "block not found")
		return
	}
	writeJSON(w, n.Blockchain.Blocks[idx])
}

func (n *Node) mempoolHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, n.Blockchain.PendingTransactions)
}

func (n *Node) balancesHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, n.Blockchain.GetBalances())
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
