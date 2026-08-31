package blockchain

import (
	"context"
	"fmt"
	"math/big"
	"runtime"
	"strings"
	"sync"
	"time"
	"toy-blockchain/transaction"
)

// Block represents a single block in the blockchain.
//
// Fields (per specification):
//   - Index        : height of this block in the chain (0 = Genesis)
//   - Timestamp    : creation time as Unix nanoseconds
//   - Transactions : ordered list of transactions included in this block
//   - PrevHash     : SHA-256 hash of the preceding block (links the chain)
//   - Nonce        : arbitrary integer reserved for future Proof-of-Work mining
//   - Hash         : SHA-256 hash of this block's own contents
type Block struct {
	Index        int                       `json:"index"`
	Timestamp    int64                     `json:"timestamp"`
	Transactions []transaction.Transaction `json:"transactions"`
	PrevHash     string                    `json:"previousHash"`
	Nonce        int                       `json:"nonce"`
	MerkleRoot   string                    `json:"merkleRoot"`
	// Difficulty records the exact difficulty used to mine this block. It is
	// stored per-block so historical blocks can be validated against the
	// same parameters they were mined with rather than relying on the
	// current network difficulty which may have since changed.
	Difficulty int `json:"difficulty"`
	// MiningTime stores the observed mining duration as nanoseconds. This
	// allows the chain to adjust future difficulty (retargeting) based on
	// how long blocks actually took to mine.
	MiningTime int64  `json:"miningTimeNanoseconds"`
	Hash       string `json:"hash"`
}

// NewBlock creates and returns a new Block.
// It automatically:
//   - sets the current Unix-nanosecond timestamp
//   - sets Nonce to 0
//   - computes the SHA-256 hash from all block fields
func NewBlock(index int, txs []transaction.Transaction, prevHash string) *Block {
	b := &Block{
		Index:        index,
		Timestamp:    time.Now().UnixNano(),
		Transactions: txs,
		PrevHash:     prevHash,
		Nonce:        0,
	}
	// Compute Merkle root from transactions before computing the block hash.
	b.MerkleRoot = CalculateMerkleRoot(b.Transactions)
	b.Hash = CalculateHash(b)
	return b
}

// Mine performs proof-of-work using multiple worker goroutines.
//
// It divides the nonce search space among several workers so that each
// worker searches unique nonces without overlap:
//
//	worker 0: 0, W, 2W, 3W, ...
//	worker 1: 1, W+1, 2W+1, 3W+1, ...
//
// where W is the worker count.
//
// The first worker to find a valid hash sends its block back over a channel,
// and cancellation is propagated to all other workers using context.
// The original block receives the successful nonce and hash before returning.
// BlockWork returns the expected proof-of-work contribution for a block.
// In a SHA-256 PoW system, a block meeting difficulty N has a target prefix of
// N leading zero bytes/hex chars, and the expected work is proportional to 16^N.
// We use exact integer arithmetic so consensus decisions never rely on floats.
func BlockWork(block *Block) *big.Int {
	if block == nil || block.Difficulty <= 0 {
		return big.NewInt(0)
	}
	return new(big.Int).Exp(big.NewInt(16), big.NewInt(int64(block.Difficulty)), nil)
}

// ChainWork returns the cumulative expected work of a chain.
// The genesis block contributes zero work; all subsequent blocks contribute
// their block work, allowing comparison of the heaviest valid chain without
// using block count as a proxy for PoW.
func ChainWork(chain []*Block) *big.Int {
	total := big.NewInt(0)
	for i, block := range chain {
		if block == nil {
			continue
		}
		if i == 0 && block.Index == 0 {
			continue
		}
		total.Add(total, BlockWork(block))
	}
	return total
}

func (b *Block) Mine(difficulty int) {
	// Store the intended difficulty on the block so the mining routines and
	// future validation use the same difficulty value. This preserves a
	// historical record of the difficulty used to mine each block.
	b.Difficulty = difficulty
	b.MineWithWorkers(difficulty, runtime.NumCPU())
}

// MineWithWorkers performs proof-of-work with configurable worker concurrency.
//
// workerCount defines how many goroutines search disjoint nonce partitions.
// A value <= 0 defaults to runtime.NumCPU().
func (b *Block) MineWithWorkers(difficulty, workerCount int) {
	b.MerkleRoot = CalculateMerkleRoot(b.Transactions)
	// Prefer the block's stored difficulty if it is set. The difficulty
	// parameter remains for compatibility, but actual work should use the
	// block's Difficulty so historical blocks remain verifiable.
	if b.Difficulty > 0 {
		difficulty = b.Difficulty
	}

	fmt.Printf("Mining Block #%d...\n\n", b.Index)
	start := time.Now()
	target := strings.Repeat("0", difficulty)
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	if workerCount <= 0 {
		workerCount = 1
	}

	ctx, cancel := context.WithCancel(context.Background()) //broadcast cancel to all workers when one finds a valid hash
	defer cancel()

	resultCh := make(chan *Block, 1) // This channel stores The first successful block

	var wg sync.WaitGroup //The WaitGroup waits until every goroutine exits.

	// Launch workers to explore disjoint nonce partitions.
	for workerID := 0; workerID < workerCount; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerBlock := *b // copy block metadata, share Transactions slice safely,Each worker gets its own copy.
			//Otherwise, all workers would modify
			workerBlock.Nonce = id //Different Starting Nonces

			for {
				select {
				case <-ctx.Done(): //If another worker has already found a valid block,
					return //and this worker immediately exits.
				default:
				}

				hash := CalculateHash(&workerBlock)
				if strings.HasPrefix(hash, target) { //Check Difficulty
					workerBlock.Hash = hash //Save Winning Hash
					select {
					case resultCh <- &workerBlock: //Only the first successful worker can send Result - channel size = 1
						cancel() //stops every other worker.
					default:
					}
					return
				}

				workerBlock.Nonce += workerCount //all workers would duplicate work.
			}
		}(workerID)
	}

	// Wait for the first successful worker, then wait for all workers to exit.
	winningBlock := <-resultCh
	wg.Wait() //Ensures every goroutine has exited before continuing

	b.Nonce = winningBlock.Nonce
	b.Hash = winningBlock.Hash

	elapsed := time.Since(start)
	// Store mining duration on the block in nanoseconds so callers can
	// inspect and use it for difficulty retargeting.
	b.MiningTime = int64(elapsed)

	fmt.Printf("Mining Time :\n%2.f seconds\n\n", elapsed.Seconds())
	fmt.Printf("Difficulty Used : %d\n\n", difficulty)
	fmt.Printf("Nonce Found : %d\n\n", b.Nonce)
	fmt.Printf("Hash        :\n%s\n\n", b.Hash)
}
