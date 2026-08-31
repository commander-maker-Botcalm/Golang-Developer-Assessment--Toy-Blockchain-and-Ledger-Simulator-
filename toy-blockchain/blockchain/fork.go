package blockchain

import (
	"fmt"
	"strings"
	"time"
	"toy-blockchain/transaction"
)

// CopyBlockchain performs a deep copy of a Blockchain. The returned copy does
// not share any slices with the original: Blocks, each Block's Transactions,
// and PendingTransactions are all independently allocated.
func CopyBlockchain(bc *Blockchain) *Blockchain {
	if bc == nil {
		return nil
	}

	copyBC := &Blockchain{
		Blocks:              make([]*Block, 0, len(bc.Blocks)),
		PendingTransactions: make([]transaction.Transaction, 0, len(bc.PendingTransactions)),
		Difficulty:          bc.Difficulty,
		BlockSize:           bc.BlockSize,
	}

	// Deep copy blocks
	for _, b := range bc.Blocks {
		if b == nil {
			copyBC.Blocks = append(copyBC.Blocks, nil)
			continue
		}
		// copy transactions slice
		txs := make([]transaction.Transaction, len(b.Transactions))
		copy(txs, b.Transactions)

		nb := &Block{
			Index:        b.Index,
			Timestamp:    b.Timestamp,
			Transactions: txs,
			PrevHash:     b.PrevHash,
			Nonce:        b.Nonce,
			MerkleRoot:   b.MerkleRoot,
			Difficulty:   b.Difficulty,
			MiningTime:   b.MiningTime,
			Hash:         b.Hash,
		}
		copyBC.Blocks = append(copyBC.Blocks, nb)
	}

	// Deep copy pending transactions
	if len(bc.PendingTransactions) > 0 {
		pts := make([]transaction.Transaction, len(bc.PendingTransactions))
		copy(pts, bc.PendingTransactions)
		copyBC.PendingTransactions = pts
	}

	return copyBC
}

// ResolveFork compares two chains and returns the chosen chain according to
// the following rules:
//  1. Validate both chains first. If only one is valid, return it.
//  2. If both are valid, choose the chain with greater cumulative proof-of-work.
//  3. If both are valid and have equal cumulative work, keep chainA as the
//     deterministic tie-breaker; block count is not used as a consensus rule.
func ResolveFork(chainA, chainB *Blockchain) *Blockchain {
	if chainA == nil && chainB == nil {
		return nil
	}
	if chainA == nil {
		return chainB
	}
	if chainB == nil {
		return chainA
	}

	errA := chainA.Validate()
	errB := chainB.Validate()

	validA := errA == nil
	validB := errB == nil

	switch {
	case validA && !validB:
		return chainA
	case !validA && validB:
		return chainB
	case !validA && !validB:
		return chainA
	default:
		workA := ChainWork(chainA.Blocks)
		workB := ChainWork(chainB.Blocks)
		if workA.Cmp(workB) > 0 {
			return chainA
		}
		if workB.Cmp(workA) > 0 {
			return chainB
		}
		return chainA
	}
}

// formatChainShort returns a compact textual summary of the chain suitable
// for printing in the fork simulation.
func formatChainShort(bc *Blockchain) string {
	if bc == nil {
		return "<nil>"
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Chain length: %d\n", len(bc.Blocks)))
	for _, b := range bc.Blocks {
		sb.WriteString(fmt.Sprintf("#%d %s prev=%s nonce=%d diff=%d time=%.2fs\n",
			b.Index, b.Hash, b.PrevHash[:8], b.Nonce, b.Difficulty, time.Duration(b.MiningTime).Seconds()))
	}
	return sb.String()
}
