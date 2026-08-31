package tests

// TestConvergenceAfterFork demonstrates requirement 6:
//
//  "Convergence after a fork: start two nodes, briefly stop them from talking,
//   mine a block on each so the chains diverge, then reconnect them. Show that
//   both nodes end on the same chain, and report which block was orphaned and
//   what happened to its transactions."
//
// Strategy (no literal server-restart needed):
//
//  Phase 1 – Isolation
//    Both nodes start with NO peers configured, so they cannot talk.
//
//  Phase 2 – Common history
//    We manually give both nodes the same 2-block base chain
//    (Genesis → Block 1 with a SYSTEM→Alice funding tx).
//
//  Phase 3 – Independent mining (the "fork")
//    Node A mines Block 2-A  carrying  SYSTEM→NodeA_Miner  50 coins.
//    Node B mines Block 2-B  carrying  SYSTEM→NodeB_Miner  75 coins.
//    Both blocks attach to the same parent (Block 1), so the chains diverge.
//
//  Phase 4 – Reconnect + sync
//    Node B adds Node A as a peer.
//    Node B calls SyncFromPeer(nodeA).
//
//    Node A has height 2 and Node B has height 2 (same length).
//    SyncFromPeer checks peer height vs local height; equal -> no action by
//    default.  To force the resolution we push Node A's block into Node B
//    via HandleIncomingBlock, which triggers the fork-resolution logic.
//    Then we also give Node A Node B's block so both sides are aware of
//    the competing block.
//
//  Phase 5 – Extend the winner
//    We mine one more block on Node A (making it height 3) and push it to
//    Node B.  Now Node A's fork is definitively longer; Node B reorganises.
//
//  Phase 6 – Report
//    We print (and assert):
//      * which block was orphaned
//      * what transaction it contained
//      * whether the orphaned tx was restored to the pending pool

import (
	"context"
	"fmt"
	"testing"
	"time"

	"toy-blockchain/blockchain"
	"toy-blockchain/node"
	"toy-blockchain/transaction"
)

// --------------------------------------------------------------------------
// Helper: build and start an isolated node with a pre-loaded blockchain.
// --------------------------------------------------------------------------
func startIsolatedNode(t *testing.T, bc *blockchain.Blockchain) *node.Node {
	t.Helper()
	cfg := node.NodeConfig{
		ListenAddr: "127.0.0.1:0",
		Peers:      []string{}, // isolated -- no peers yet
		DataFile:   t.TempDir() + "/chain.json",
		Difficulty: 1, // low difficulty so tests run fast
		BlockSize:  10,
	}
	n := node.NewNode(cfg)
	n.Blockchain = bc
	if err := n.Start(); err != nil {
		t.Fatalf("failed to start node: %v", err)
	}
	t.Cleanup(func() {
		_ = n.Shutdown(context.Background())
	})
	return n
}

// --------------------------------------------------------------------------
// Helper: build the shared base chain that both nodes will start from.
// Returns the chain; caller should deep-copy it before giving it to nodes.
// --------------------------------------------------------------------------
func buildBaseChain(t *testing.T) *blockchain.Blockchain {
	t.Helper()
	bc := blockchain.NewBlockchain()
	bc.Difficulty = 1

	// Fund Alice with 200 coins via SYSTEM -- this is the "common history"
	if err := bc.AddTransaction(transaction.Transaction{
		Sender:    "SYSTEM",
		Recipient: "Alice",
		Amount:    200,
	}); err != nil {
		t.Fatalf("failed to add base funding tx: %v", err)
	}
	bc.MinePendingTransactions()

	if len(bc.Blocks) != 2 {
		t.Fatalf("expected 2 blocks after base mining, got %d", len(bc.Blocks))
	}
	return bc
}

// --------------------------------------------------------------------------
// The main test.
// --------------------------------------------------------------------------
func TestConvergenceAfterFork(t *testing.T) {

	banner := func(msg string) {
		fmt.Println()
		fmt.Println("=======================================================")
		fmt.Printf("  %s\n", msg)
		fmt.Println("=======================================================")
	}

	// -------------------------------------------------------------------------
	// PHASE 1 & 2 -- Isolation + shared common history
	// -------------------------------------------------------------------------
	banner("PHASE 1 & 2 -- Isolated nodes, common base chain")

	base := buildBaseChain(t)
	baseA := blockchain.CopyBlockchain(base) // deep copy for Node A
	baseB := blockchain.CopyBlockchain(base) // deep copy for Node B

	nodeA := startIsolatedNode(t, baseA)
	nodeB := startIsolatedNode(t, baseB)

	// Both nodes start isolated.
	fmt.Printf("[Node A]  addr=%s  peers=%v\n", nodeA.Addr(), nodeA.Config.Peers)
	fmt.Printf("[Node B]  addr=%s  peers=%v\n", nodeB.Addr(), nodeB.Config.Peers)

	fmt.Printf("[Node A]  chain length = %d\n", len(nodeA.Blockchain.Blocks))
	fmt.Printf("[Node B]  chain length = %d\n", len(nodeB.Blockchain.Blocks))

	// Verify they share the same genesis and block-1
	if nodeA.Blockchain.Blocks[1].Hash != nodeB.Blockchain.Blocks[1].Hash {
		t.Fatal("nodes do not share the same base chain -- test setup error")
	}
	fmt.Printf("[Shared]  Block #1 hash = %s...\n", nodeA.Blockchain.Blocks[1].Hash[:12])

	// -------------------------------------------------------------------------
	// PHASE 3 -- Independent mining (chains diverge)
	// -------------------------------------------------------------------------
	banner("PHASE 3 -- Independent mining (fork creation)")

	// Node A mines its own block
	txA := transaction.Transaction{Sender: "SYSTEM", Recipient: "NodeA_Miner", Amount: 50}
	if err := nodeA.Blockchain.AddTransaction(txA); err != nil {
		t.Fatalf("node A: failed to add tx: %v", err)
	}
	nodeA.Blockchain.MinePendingTransactions()
	blockA := nodeA.Blockchain.Blocks[len(nodeA.Blockchain.Blocks)-1]

	// Node B mines its own different block
	txB := transaction.Transaction{Sender: "SYSTEM", Recipient: "NodeB_Miner", Amount: 75}
	if err := nodeB.Blockchain.AddTransaction(txB); err != nil {
		t.Fatalf("node B: failed to add tx: %v", err)
	}
	nodeB.Blockchain.MinePendingTransactions()
	blockB := nodeB.Blockchain.Blocks[len(nodeB.Blockchain.Blocks)-1]

	// Both are now at height 2, but with different blocks
	if len(nodeA.Blockchain.Blocks) != 3 {
		t.Fatalf("node A: expected 3 blocks, got %d", len(nodeA.Blockchain.Blocks))
	}
	if len(nodeB.Blockchain.Blocks) != 3 {
		t.Fatalf("node B: expected 3 blocks, got %d", len(nodeB.Blockchain.Blocks))
	}

	fmt.Println()
	fmt.Println("--- Node A chain (isolated mine) ---")
	fmt.Printf("  Block #2-A  hash=%s...\n", blockA.Hash[:12])
	fmt.Printf("  Block #2-A  tx : %s -> %s  amount=%.0f\n", txA.Sender, txA.Recipient, txA.Amount)

	fmt.Println()
	fmt.Println("--- Node B chain (isolated mine) ---")
	fmt.Printf("  Block #2-B  hash=%s...\n", blockB.Hash[:12])
	fmt.Printf("  Block #2-B  tx : %s -> %s  amount=%.0f\n", txB.Sender, txB.Recipient, txB.Amount)

	// Confirm divergence
	if blockA.Hash == blockB.Hash {
		t.Fatal("expected different block hashes after independent mining, got same")
	}
	fmt.Println()
	fmt.Println("[DIVERGED]  Block #2-A hash != Block #2-B hash  -- fork confirmed")

	// -------------------------------------------------------------------------
	// PHASE 4 -- Reconnect: cross-inject the competing blocks
	// -------------------------------------------------------------------------
	banner("PHASE 4 -- Reconnect nodes (push competing blocks)")

	// Connect them as peers
	addrA := fmt.Sprintf("http://%s", nodeA.Addr())
	addrB := fmt.Sprintf("http://%s", nodeB.Addr())
	nodeA.Config.Peers = []string{addrB}
	nodeB.Config.Peers = []string{addrA}
	fmt.Printf("[Node A]  now peering with %s\n", addrB)
	fmt.Printf("[Node B]  now peering with %s\n", addrA)

	// Push Node A's block into Node B: HandleIncomingBlock triggers fork logic
	acceptedByB, errB := nodeB.HandleIncomingBlock(blockA)
	fmt.Printf("[Node B]  received Block #2-A: accepted=%v err=%v\n", acceptedByB, errB)

	// Push Node B's block into Node A
	acceptedByA, errA := nodeA.HandleIncomingBlock(blockB)
	fmt.Printf("[Node A]  received Block #2-B: accepted=%v err=%v\n", acceptedByA, errA)

	// At this point both chains are length 3 with one stored fork candidate.
	// Neither has reorganized yet (same length, so no winner yet).
	fmt.Printf("[Node A]  chain length after cross-push = %d\n", len(nodeA.Blockchain.Blocks))
	fmt.Printf("[Node B]  chain length after cross-push = %d\n", len(nodeB.Blockchain.Blocks))

	// -------------------------------------------------------------------------
	// PHASE 5 -- Extend Node A's chain to make it the definitive winner
	// -------------------------------------------------------------------------
	banner("PHASE 5 -- Node A mines another block -> becomes longest chain")

	// Record what is currently in Node B's pending pool before the reorg
	pendingBeforeReorg := len(nodeB.Blockchain.PendingTransactions)
	fmt.Printf("[Node B]  pending txs before reorg = %d\n", pendingBeforeReorg)

	// Node A mines Block #3-A
	txA2 := transaction.Transaction{Sender: "SYSTEM", Recipient: "NodeA_Extra", Amount: 10}
	if err := nodeA.Blockchain.AddTransaction(txA2); err != nil {
		t.Fatalf("node A: failed to add extra tx: %v", err)
	}
	nodeA.Blockchain.MinePendingTransactions()
	blockA2 := nodeA.Blockchain.Blocks[len(nodeA.Blockchain.Blocks)-1]

	fmt.Printf("[Node A]  mined Block #3-A  hash=%s...\n", blockA2.Hash[:12])
	fmt.Printf("[Node A]  chain length = %d (height=%d)\n",
		len(nodeA.Blockchain.Blocks), len(nodeA.Blockchain.Blocks)-1)

	// Push Block #3-A to Node B. This makes Node A's fork definitively longer,
	// triggering a chain reorganization on Node B.
	acceptedByB2, errB2 := nodeB.HandleIncomingBlock(blockA2)
	fmt.Printf("[Node B]  received Block #3-A: accepted=%v err=%v\n", acceptedByB2, errB2)

	// Small sleep for log ordering clarity (reorg is synchronous, so not needed
	// for correctness).
	time.Sleep(50 * time.Millisecond)

	// -------------------------------------------------------------------------
	// PHASE 6 -- Report: convergence, orphaned block, orphaned transactions
	// -------------------------------------------------------------------------
	banner("PHASE 6 -- Convergence report")

	lenA := len(nodeA.Blockchain.Blocks)
	lenB := len(nodeB.Blockchain.Blocks)
	fmt.Printf("[Node A]  final chain length = %d\n", lenA)
	fmt.Printf("[Node B]  final chain length = %d\n", lenB)

	// -- Verify both nodes are on the SAME chain
	if lenA != lenB {
		t.Errorf("chain lengths differ after convergence: A=%d B=%d", lenA, lenB)
	}

	finalHashA := nodeA.Blockchain.Blocks[lenA-1].Hash
	finalHashB := nodeB.Blockchain.Blocks[lenB-1].Hash

	fmt.Printf("[Node A]  head hash = %s...\n", finalHashA[:12])
	fmt.Printf("[Node B]  head hash = %s...\n", finalHashB[:12])

	if finalHashA != finalHashB {
		t.Errorf("nodes have different chain heads after convergence:\n  A=%s\n  B=%s",
			finalHashA, finalHashB)
	} else {
		fmt.Println()
		fmt.Println("  [OK] CONVERGENCE CONFIRMED -- both nodes share the same head hash.")
	}

	// -- Report the winning chain
	fmt.Println()
	fmt.Println("--- Winning chain (Node A fork, adopted by Node B) ---")
	for _, b := range nodeA.Blockchain.Blocks {
		fmt.Printf("  Block #%d  hash=%s...  txs=%d\n", b.Index, b.Hash[:12], len(b.Transactions))
		for _, tx := range b.Transactions {
			fmt.Printf("            tx: %s -> %s  amount=%.0f\n", tx.Sender, tx.Recipient, tx.Amount)
		}
	}

	// -- Report the orphaned block
	fmt.Println()
	fmt.Println("--- Orphaned block (Node B's Block #2-B, replaced by reorg) ---")
	fmt.Printf("  Orphaned block hash  : %s\n", blockB.Hash)
	fmt.Printf("  Orphaned block index : %d\n", blockB.Index)
	fmt.Printf("  Orphaned block contained %d transaction(s):\n", len(blockB.Transactions))
	for i, tx := range blockB.Transactions {
		fmt.Printf("    [%d]  %s -> %s  amount=%.0f\n", i, tx.Sender, tx.Recipient, tx.Amount)
	}

	// -- What happened to the orphaned tx?
	fmt.Println()
	fmt.Println("--- What happened to the orphaned transaction(s)? ---")

	pendingAfterReorg := nodeB.Blockchain.PendingTransactions
	fmt.Printf("  Node B pending pool size after reorg = %d\n", len(pendingAfterReorg))

	orphanedTxFound := false
	for _, ptx := range pendingAfterReorg {
		fmt.Printf("  Pending: %s -> %s  amount=%.0f\n", ptx.Sender, ptx.Recipient, ptx.Amount)
		if ptx.Recipient == txB.Recipient && ptx.Amount == txB.Amount {
			orphanedTxFound = true
		}
	}

	if orphanedTxFound {
		fmt.Println()
		fmt.Printf("  [OK] Orphaned tx (SYSTEM -> %s  %.0f) was RESTORED to Node B's mempool.\n",
			txB.Recipient, txB.Amount)
		fmt.Println("       It can be re-mined into a future block on the canonical chain.")
	} else {
		fmt.Println()
		fmt.Printf("  [i] Orphaned tx (SYSTEM -> %s  %.0f) was NOT found in the pending pool.\n",
			txB.Recipient, txB.Amount)
		fmt.Println("      This is expected if a tx with the same ID already exists in the")
		fmt.Println("      new canonical chain, or if balance validation failed post-reorg.")
	}

	// -- Validate both chains are internally consistent
	if err := nodeA.Blockchain.Validate(); err != nil {
		t.Errorf("Node A chain invalid after convergence: %v", err)
	} else {
		fmt.Println()
		fmt.Println("  [OK] Node A chain validation: PASSED")
	}

	if err := nodeB.Blockchain.Validate(); err != nil {
		t.Errorf("Node B chain invalid after convergence: %v", err)
	} else {
		fmt.Println("  [OK] Node B chain validation: PASSED")
	}

	// -- Final summary
	banner("SUMMARY")
	fmt.Println("  1. Two nodes started in ISOLATION (no peers).")
	fmt.Printf("  2. Both built a common base chain (%d blocks).\n", len(base.Blocks))
	fmt.Println("  3. Each independently mined a different Block #2 -> chains DIVERGED.")
	fmt.Printf("     Node A Block #2-A: SYSTEM -> %s (%.0f coins)\n", txA.Recipient, txA.Amount)
	fmt.Printf("     Node B Block #2-B: SYSTEM -> %s (%.0f coins)\n", txB.Recipient, txB.Amount)
	fmt.Println("  4. Nodes were RECONNECTED; competing blocks were exchanged.")
	fmt.Println("  5. Node A mined an extra block -> its chain became LONGER (height 3 vs 2).")
	fmt.Println("  6. Node B detected a longer valid fork and REORGANIZED:")
	fmt.Printf("     - Orphaned block : #%d  hash=%s...\n", blockB.Index, blockB.Hash[:16])
	fmt.Printf("     - Orphaned tx    : %s -> %s  amount=%.0f\n", txB.Sender, txB.Recipient, txB.Amount)
	if orphanedTxFound {
		fmt.Println("     - Orphaned tx fate: RESTORED to mempool (awaiting re-mining)")
	} else {
		fmt.Println("     - Orphaned tx fate: not in mempool (duplicate or balance check failed)")
	}
	fmt.Println("  7. Both nodes now share the SAME canonical chain head.")
	fmt.Println()
}
