package blockchain

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"toy-blockchain/transaction"
)

// CalculateHash produces a deterministic SHA-256 hash for a given Block.
//
// # Serialization contract
//
// Fields are joined in this exact order with "|" as a separator:
//
//	Index | Timestamp | Transactions | PreviousHash | Nonce
//
// The Hash field itself is intentionally EXCLUDED — it is the output of
// this function, not part of its input. Including it would be circular.
//
// Each field is formatted as follows:
//
//	Index        → decimal integer          e.g. "3"
//	Timestamp    → decimal integer          e.g. "1783319207065260400"
//	Transactions → pipe-delimited records,  e.g. "Alice|Bob|10.00000000|Carol|Dave|5.00000000"
//	               each tx: Sender|Recipient|Amount(8dp)
//	PreviousHash → hex string as-is         e.g. "0000...0000"
//	Nonce        → decimal integer          e.g. "0"
//
// The "|" separator between every field guarantees no two distinct inputs
// can ever produce the same byte sequence (prevents length-extension collisions).
func CalculateHash(b *Block) string {
	// Use MerkleRoot instead of serializing full tx list.
	// If MerkleRoot is not set on the block (some callers construct Block directly),
	// compute it deterministically from the Transactions slice so behavior remains
	// backward-compatible: changing Transactions will affect the hash.
	merkle := b.MerkleRoot
	if merkle == "" {
		merkle = CalculateMerkleRoot(b.Transactions)
	}

	record := strings.Join([]string{
		fmt.Sprintf("%d", b.Index),
		fmt.Sprintf("%d", b.Timestamp),
		merkle,
		b.PrevHash,
		fmt.Sprintf("%d", b.Nonce),
	}, "|")

	hash := sha256.Sum256([]byte(record))
	return fmt.Sprintf("%x", hash) // 64 lowercase hex chars
}

// HashTransaction deterministically serializes and hashes a single transaction
// using the existing serialization format: "Sender|Recipient|Amount(8dp)".
// It returns the lowercase hex SHA-256 digest.
func HashTransaction(tx transaction.Transaction) string {
	record := fmt.Sprintf("%s|%s|%.8f", tx.Sender, tx.Recipient, tx.Amount)
	sum := sha256.Sum256([]byte(record))
	return fmt.Sprintf("%x", sum)
}

// CalculateMerkleRoot computes the Merkle root for a slice of transactions.
// Rules:
// - empty slice → SHA-256("")
// - each transaction is hashed individually using HashTransaction
// - adjacent pairs are concatenated (left||right) and SHA-256 hashed to build the next level
// - if a level has an odd number of nodes, the last node is duplicated
// - repeat until a single root remains
func CalculateMerkleRoot(txs []transaction.Transaction) string {
	if len(txs) == 0 {
		sum := sha256.Sum256([]byte(""))
		return fmt.Sprintf("%x", sum)
	}

	// compute leaf hashes
	hashes := make([]string, 0, len(txs))
	for _, tx := range txs {
		hashes = append(hashes, HashTransaction(tx))
	}

	// iteratively compute parent levels
	for len(hashes) > 1 {
		if len(hashes)%2 == 1 {
			// duplicate last
			hashes = append(hashes, hashes[len(hashes)-1])
		}
		var next []string
		for i := 0; i < len(hashes); i += 2 {
			left := hashes[i]
			right := hashes[i+1]
			concat := left + right
			sum := sha256.Sum256([]byte(concat))
			next = append(next, fmt.Sprintf("%x", sum))
		}
		hashes = next
	}

	return hashes[0]
}

// (removed) serializeTransactions is no longer used — Merkle roots are used instead.
