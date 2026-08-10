package blockchain

import (
	"bytes"         //helps work with byte slices, used here for trimming whitespace when reading a file.
	"encoding/json" //handles JSON encoding and decoding, used for saving and loading the blockchain.
	"fmt"
	"os"            //interacts with the operating system, used for reading and writing files.
	"path/filepath" //handles file paths in a platform-safe way, used when creating directories for save files.
	"strings"       //provides string operations, used here to check proof-of-work difficulty by repeating zeros and checking prefixes.
	"time"          //works with dates and durations, used for block timestamps and mining-time calculations.
	"toy-blockchain/transaction"
)

// GenesisPrevHash is the canonical previous-hash value for the Genesis block.
// It is a 64-character string of zeros — representing "no prior block".
// Using a fixed sentinel (rather than an empty string) matches the Bitcoin convention
// and makes it immediately obvious in logs/output that this is the chain origin.
const GenesisPrevHash = "0000000000000000000000000000000000000000000000000000000000000000"

// DefaultDifficulty defines the number of leading zeros required for a block's hash to be valid (Proof-of-Work).
const DefaultDifficulty = 4

// DefaultBlockSize limits how many transactions are mined into a single block.
const DefaultBlockSize = 10

// TargetBlockTime is the target duration we want each block to take to mine.
// The retargeting algorithm increases difficulty when blocks are mined faster
// than this target and decreases difficulty when they are slower.
var TargetBlockTime = 1 * time.Second

// AdjustDifficulty is a simple retargeting algorithm that adjusts the
// difficulty based on the observed miningTime relative to the target.
// It never allows difficulty to drop below 1.
func AdjustDifficulty(currentDifficulty int, miningTime time.Duration, target time.Duration) int {
	next := currentDifficulty
	if miningTime < target {
		next = currentDifficulty + 1
	} else if miningTime > target && currentDifficulty > 1 {
		next = currentDifficulty - 1
	}
	if next < 1 {
		next = 1
	}
	return next
}

// recentAverageMiningTime computes the average mining time (duration) of the
// most recent `n` mined blocks, excluding the genesis block and any blocks
// that do not have a recorded MiningTime (>0). If no valid samples exist,
// it returns 0.
func recentAverageMiningTime(bc *Blockchain, n int) time.Duration {
	if n <= 0 {
		n = 3
	}
	var sum time.Duration
	var count int
	// iterate backwards skipping genesis (index 0)
	for i := len(bc.Blocks) - 1; i >= 1 && count < n; i-- {
		mt := time.Duration(bc.Blocks[i].MiningTime)
		if mt <= 0 {
			continue
		}
		sum += mt
		count++
	}
	if count == 0 {
		return 0
	}
	return time.Duration(int64(sum) / int64(count))
}

// Blockchain is an ordered slice of Block pointers.
// The first element (index 0) is always the Genesis block.
type Blockchain struct {
	Blocks              []*Block                  `json:"blocks"`
	PendingTransactions []transaction.Transaction `json:"pendingTransactions"`
	Difficulty          int                       `json:"difficulty"`
	BlockSize           int                       `json:"blockSize"`
}

// NewBlockchain is the constructor for a fresh Blockchain.
//
// It follows this exact flow, step by step:
//
//	┌─────────────────────┐
//	│   New Blockchain    │  ← allocate the Blockchain struct
//	└────────┬────────────┘
//	         ↓
//	┌─────────────────────┐
//	│ Create Genesis Block│  ← build block with fixed spec values
//	└────────┬────────────┘
//	         ↓
//	┌─────────────────────┐
//	│  Calculate Hash     │  ← SHA-256 over Index|Timestamp|Txs|PrevHash|Nonce
//	└────────┬────────────┘
//	         ↓
//	┌─────────────────────┐
//	│ Store Genesis Block │  ← append to Blocks slice
//	└────────┬────────────┘
//	         ↓
//	┌─────────────────────┐
//	│  Return Blockchain  │  ← hand back the ready chain
//	└─────────────────────┘
func NewBlockchain() *Blockchain {
	return NewBlockchainWithConfig(DefaultDifficulty, DefaultBlockSize)
}

// NewBlockchainWithConfig creates a fresh blockchain using the supplied mining settings.
func NewBlockchainWithConfig(difficulty, blockSize int) *Blockchain {
	// Step 1 — New Blockchain: allocate an empty chain.
	bc := &Blockchain{
		Blocks:              []*Block{},
		PendingTransactions: []transaction.Transaction{},
		Difficulty:          difficulty,
		BlockSize:           blockSize,
	}
	bc.applyDefaults()

	// Step 2 — Create Genesis Block: build the block with all fixed spec values.
	//           genesisBlock() returns the block WITHOUT a hash yet — the hash
	//           step is deliberately kept here so the flow is explicit.
	genesis := newGenesisBlock()
	// Record the initial difficulty on the genesis block so the chain has a
	// complete history of difficulty values from the start. This also ensures
	// validation will check the genesis block against the difficulty used to
	// create it.
	genesis.Difficulty = bc.Difficulty

	// Step 3 — Calculate Merkle root and Hash: compute Merkle root from txs,
	// then compute SHA-256 over the five input fields. This is the only place
	// the genesis hash is produced.
	genesis.MerkleRoot = CalculateMerkleRoot(genesis.Transactions)
	genesis.Hash = CalculateHash(genesis)

	// Step 4 — Store Genesis Block: attach it as the first (and only) block.
	bc.Blocks = append(bc.Blocks, genesis)

	// Step 5 — Return Blockchain.
	return bc
}

// newGenesisBlock builds the Genesis Block struct with hard-coded spec values.
//
// It intentionally does NOT compute the hash — that is the constructor's job
// (Step 3 above). This keeps each step of the flow single-responsibility.
//
// Fixed values (per specification):
//
//	Index        = 0
//	Timestamp    = 0              (reproducible — NOT time.Now())
//	Transactions = []             (Genesis carries no transactions)
//	PrevHash     = GenesisPrevHash  (64 zeros — no prior block)
//	Nonce        = 0
func newGenesisBlock() *Block {
	return &Block{
		Index:        0,
		Timestamp:    0,
		Transactions: []transaction.Transaction{},
		PrevHash:     GenesisPrevHash,
		Nonce:        0,
		// Hash is intentionally left empty — filled in by NewBlockchain Step 3.
	}
}

func (bc *Blockchain) applyDefaults() {
	if bc.Difficulty <= 0 {
		bc.Difficulty = DefaultDifficulty
	}
	if bc.BlockSize <= 0 {
		bc.BlockSize = DefaultBlockSize
	}
}

// balances computes the balance for each account by walking the blockchain
// and optionally including pending transactions.
func (bc *Blockchain) balances(includePending bool) map[string]float64 {
	balances := make(map[string]float64)
	for _, block := range bc.Blocks {
		for _, tx := range block.Transactions {
			if tx.Sender != "SYSTEM" {
				balances[tx.Sender] -= tx.Amount
			}
			balances[tx.Recipient] += tx.Amount
		}
	}
	if includePending {
		for _, tx := range bc.PendingTransactions {
			if tx.Sender != "SYSTEM" {
				balances[tx.Sender] -= tx.Amount
			}
			balances[tx.Recipient] += tx.Amount
		}
	}
	return balances
}

// AddTransaction validates a transaction and adds it to the pending transactions pool.
// For non-SYSTEM transactions, it verifies the cryptographic signature before acceptance.
func (bc *Blockchain) AddTransaction(tx transaction.Transaction) error {
	if tx.Amount <= 0 {
		return fmt.Errorf("transaction amount must be greater than zero, got %f", tx.Amount)
	}
	if tx.Sender != "SYSTEM" {
		valid, err := transaction.VerifyTransaction(&tx)
		if err != nil {
			return fmt.Errorf("signature verification error: %v", err)
		}
		if !valid {
			return fmt.Errorf("invalid transaction signature")
		}
		balances := bc.balances(true)
		if balances[tx.Sender] < tx.Amount {
			return fmt.Errorf("insufficient balance for sender %s: has %.8f, wants to send %.8f", tx.Sender, balances[tx.Sender], tx.Amount)
		}
	}
	bc.PendingTransactions = append(bc.PendingTransactions, tx)
	return nil
}

const DefaultDataFile = "blockchain.json"

// GetBalances computes the balance for each account by walking the entire blockchain
// and accumulating the amounts from all confirmed transactions.
func (bc *Blockchain) GetBalances() map[string]float64 {
	return bc.balances(false)
}

func LoadFromFile(path string) (*Blockchain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return NewBlockchain(), nil
		}
		return nil, err
	}

	if len(bytes.TrimSpace(data)) == 0 {
		return NewBlockchain(), nil
	}

	var bc Blockchain
	if err := json.Unmarshal(data, &bc); err != nil {
		return nil, err
	}
	bc.applyDefaults()
	if len(bc.Blocks) == 0 {
		return NewBlockchainWithConfig(bc.Difficulty, bc.BlockSize), nil
	}
	if bc.PendingTransactions == nil {
		bc.PendingTransactions = []transaction.Transaction{}
	}
	return &bc, nil
}

func LoadBlockchain(path string) (*Blockchain, error) {
	return LoadFromFile(path)
}

func (bc *Blockchain) SaveToFile(path string) error {
	data, err := json.MarshalIndent(bc, "", "  ")
	if err != nil {
		return err
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// AddBlock appends a new block containing the supplied transactions.
// It is a convenience wrapper for the pending-transaction flow:
//  1. Append txs to PendingTransactions
//  2. MinePendingTransactions()
func (bc *Blockchain) AddBlock(txs []transaction.Transaction) {
	bc.PendingTransactions = append(bc.PendingTransactions, txs...)
	bc.MinePendingTransactions()
}

// MinePendingTransactions creates a new block from the pending transaction pool,
// mines it using the blockchain's configured difficulty and block size, appends it
// to the chain, and leaves any unmined transactions pending.
func (bc *Blockchain) MinePendingTransactions() {
	if len(bc.PendingTransactions) == 0 {
		return
	}

	bc.applyDefaults()
	prev := bc.Blocks[len(bc.Blocks)-1] //Gets the most recent block in the chain so the new block can link to it.
	maxTransactions := len(bc.PendingTransactions)
	if bc.BlockSize > 0 && bc.BlockSize < maxTransactions {
		maxTransactions = bc.BlockSize
	}

	transactionsToMine := append([]transaction.Transaction(nil), bc.PendingTransactions[:maxTransactions]...)
	newBlock := NewBlock(prev.Index+1, transactionsToMine, prev.Hash)

	// Set the block's Difficulty to the current network difficulty before
	// mining. This ensures the block records which difficulty was used so it
	// can be validated later even if the network difficulty changes.
	newBlock.Difficulty = bc.Difficulty

	newBlock.Mine(bc.Difficulty)

	// Append the block first so recentAverageMiningTime can observe it.
	bc.Blocks = append(bc.Blocks, newBlock)

	// Compute a stable average mining time over the most recent few blocks
	// (including the one we just mined). This avoids reacting to a single
	// very-fast sample and stabilizes retargeting.
	avg := recentAverageMiningTime(bc, 3)
	// If we couldn't compute an average (no valid samples), fall back to
	// this block's measured time.
	if avg <= 0 {
		avg = time.Duration(newBlock.MiningTime)
	}

	// After mining, adjust the network difficulty for the next block based
	// on the averaged mining time.
	nextDifficulty := AdjustDifficulty(bc.Difficulty, avg, TargetBlockTime)

	// Print a concise summary including the next difficulty so users can
	// observe retargeting in action.
	fmt.Println("--------------------------------------------------")
	fmt.Printf("Block #%d mined\n", newBlock.Index)
	fmt.Printf("Mining Time : %.2f seconds\n", time.Duration(newBlock.MiningTime).Seconds())
	fmt.Printf("Avg Mining Time (used for retarget) : %.2f seconds\n", avg.Seconds())
	fmt.Printf("Difficulty Used : %d\n", newBlock.Difficulty)
	fmt.Printf("Next Difficulty : %d\n", nextDifficulty)
	fmt.Printf("Nonce : %d\n", newBlock.Nonce)
	fmt.Println("--------------------------------------------------")

	// Apply the retarget for the next block.
	bc.Difficulty = nextDifficulty

	if maxTransactions < len(bc.PendingTransactions) {
		bc.PendingTransactions = append([]transaction.Transaction(nil), bc.PendingTransactions[maxTransactions:]...)
	} else {
		bc.PendingTransactions = []transaction.Transaction{}
	}
}

// Validate checks the full blockchain for integrity.
// It verifies:
//  1. Stored hash matches recalculated hash.
//  2. Each block's PrevHash matches the previous block's Hash.
//  3. Each mined block satisfies the proof-of-work difficulty.
//  4. Block indexes increase sequentially.
//  5. Block timestamps are non-decreasing.
func (bc *Blockchain) Validate() error {
	if len(bc.Blocks) == 0 {
		return fmt.Errorf("blockchain contains no blocks")
	}

	bc.applyDefaults()

	for i, current := range bc.Blocks {
		// Verify MerkleRoot matches the transactions to detect tampering.
		recomputedMerkle := CalculateMerkleRoot(current.Transactions)
		if current.MerkleRoot != recomputedMerkle {
			return fmt.Errorf("invalid block %d: merkle root does not match transactions", current.Index)
		}
		if current.Hash != CalculateHash(current) {
			return fmt.Errorf("invalid block %d: stored hash does not match calculated hash", current.Index)
		}

		if i == 0 {
			if current.Index != 0 {
				return fmt.Errorf("invalid genesis block index: expected 0, got %d", current.Index)
			}
			if current.Timestamp != 0 {
				return fmt.Errorf("invalid genesis block timestamp: expected 0, got %d", current.Timestamp)
			}
			if current.PrevHash != GenesisPrevHash {
				return fmt.Errorf("invalid genesis block previous hash: expected %s, got %s", GenesisPrevHash, current.PrevHash)
			}
			if len(current.Transactions) != 0 {
				return fmt.Errorf("invalid genesis block: expected no transactions, got %d", len(current.Transactions))
			}
			if current.Nonce != 0 {
				return fmt.Errorf("invalid genesis block nonce: expected 0, got %d", current.Nonce)
			}
			continue
		}

		prev := bc.Blocks[i-1]
		if current.Index != prev.Index+1 {
			return fmt.Errorf("invalid block %d index: expected %d", current.Index, prev.Index+1)
		}
		if current.PrevHash != prev.Hash {
			return fmt.Errorf("invalid block %d: previous hash does not match hash of block %d", current.Index, prev.Index)
		}
		// Use the difficulty that was recorded on the block when it was
		// mined rather than the current network difficulty. Historical
		// blocks must validate against the parameters they were mined with.
		blockTarget := strings.Repeat("0", current.Difficulty)
		if !strings.HasPrefix(current.Hash, blockTarget) {
			return fmt.Errorf("invalid block %d: proof of work not satisfied", current.Index)
		}
		if current.Timestamp < prev.Timestamp {
			return fmt.Errorf("invalid block %d: timestamp is earlier than previous block", current.Index)
		}
	}

	return nil
}

// IsValid walks the chain to verify that:
//  1. The link between consecutive blocks is unbroken (PrevHash == previous block's Hash).
//  2. The data of each block hasn't been tampered with (Hash matches CalculateHash).
//  3. Each mined block satisfies the difficulty requirement (except the Genesis block).
func (bc *Blockchain) IsValid() bool {
	return bc.Validate() == nil
}

// separator is the divider line used between blocks in the printed output.
const separator = "------------------------"

// Print outputs each block in the chain to stdout in the canonical format:
//
//	------------------------
//	Block #N
//	Timestamp    : <unix-nano>
//	Previous     : <64-char hex>
//	Nonce        : <int>
//	Hash         : <64-char hex>
//	Transactions : <count>
//	  [0] Alice → Bob : 10.00000000   ← only shown when count > 0
//	------------------------
//
// Use this to verify the Genesis block is created correctly (Block #0 must
// always show Timestamp=0, Previous=0000...0000, Transactions=0).
func (bc *Blockchain) Print() {
	for _, b := range bc.Blocks {
		fmt.Println(separator)
		fmt.Printf("Block #%d\n", b.Index)
		fmt.Printf("Timestamp    : %d\n", b.Timestamp)
		fmt.Printf("Previous     : %s\n", b.PrevHash)
		fmt.Printf("Nonce        : %d\n", b.Nonce)
		fmt.Printf("Hash         : %s\n", b.Hash)
		fmt.Printf("Transactions : %d\n", len(b.Transactions))

		// Print individual transactions beneath the count line.
		for i, tx := range b.Transactions {
			fmt.Printf("  [%d] %s\n", i, tx)
		}
	}
	fmt.Println(separator)
}
