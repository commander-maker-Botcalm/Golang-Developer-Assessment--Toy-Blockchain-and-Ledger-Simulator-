# Toy Blockchain: Design Write-Up

## 1. Wire Format and Endpoints

### 1.1 Data Serialization (Wire Format)

All communication between nodes uses **JSON over HTTP/REST**. The blockchain system serializes all entities to JSON:

#### Block Wire Format
```json
{
  "index": 1,
  "timestamp": 1693123456789000000,
  "transactions": [
    {
      "sender": "Alice",
      "recipient": "Bob", 
      "amount": 50,
      "publicKey": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...",
      "signature": "3045022100d7e5c3f9a2b1e4d6c3a1f2e9b4d5c3a2f1e9b4d5022050e3f2d1c0b9a8f7e6d5c4b3a2f1e9d8c7b6a5f4"
    }
  ],
  "previousHash": "0000000000000000000000000000000000000000000000000000000000000000",
  "nonce": 12847,
  "merkleRoot": "a7b3c2d1e9f4a6b8c5d2e3f0a1b4c7d9e2f5a8b",
  "difficulty": 4,
  "miningTimeNanoseconds": 2345678901,
  "hash": "0000a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r"
}
```

#### Transaction Wire Format
```json
{
  "sender": "Alice",
  "recipient": "Bob",
  "amount": 50,
  "publicKey": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...",
  "signature": "3045022100d7e5c3f9a2b1e4d6c3a1f2e9b4d5c3a2f1e9b4d5022050e3f2d1c0b9a8f7e6d5c4b3a2f1e9d8c7b6a5f4"
}
```

**Cryptographic Components:**
- **PublicKey**: ECDSA P-256 public key, hex-encoded from PEM format
- **Signature**: ECDSA signature over SHA-256(sender | recipient | amount), hex-encoded
- **SYSTEM transactions** bypass signature verification (no sender key needed)

### 1.2 HTTP Endpoints

All endpoints use standard HTTP methods and return JSON responses. The node listens on a configurable TCP address (e.g., `localhost:8081`).

#### Read-Only Endpoints

| Endpoint | Method | Description | Returns |
|----------|--------|-------------|---------|
| `/health` | GET | Liveness check | `"OK"` (status 200) |
| `/status` | GET | Node metadata | `{peers, data_file, difficulty, block_size, chain_length}` |
| `/peers` | GET | Connected peers | `["http://localhost:8082", ...]` |
| `/chain` | GET | Full blockchain | `[Block, Block, ...]` (JSON array of all blocks) |
| `/chain/info` | GET | Chain summary | `{height: <int>, head_hash: "<hex>"}` |
| `/block/<index>` | GET | Single block by index | `Block` (JSON object) or 404 |
| `/mempool` | GET | Pending transactions | `[Transaction, Transaction, ...]` |
| `/balances` | GET | All account balances | `{sender: amount, recipient: amount, ...}` |
| `/blocks/range` | GET | Block range query | `[Block, Block, ...]` for indices `?from=X&to=Y` |

#### Write Endpoints

| Endpoint | Method | Description | Body Format | Response |
|----------|--------|-------------|------------|----------|
| `/transactions` | POST | Broadcast transaction | `Transaction` (JSON) | `{"status":"accepted"}` (202) or `{"status":"duplicate"}` (200) |
| `/blocks` | POST | Broadcast block | `Block` (JSON) | `{"status":"accepted"}` (202) or `{"status":"duplicate"}` (200) |
| `/mine` | POST | Mine pending transactions | (empty) | `{"status":"mined", "index":<n>, "hash":"..."}` |
| `/sync` | GET | Manual peer sync | `?peer=<URL>` (query param) | `{"status":"synced", "peer":"..."}` |

#### Example Requests

**Broadcast a transaction:**
```bash
curl -X POST http://localhost:8081/transactions \
  -H "Content-Type: application/json" \
  -d '{
    "sender": "Alice",
    "recipient": "Bob",
    "amount": 50,
    "publicKey": "MFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAE...",
    "signature": "3045022100d7e5c3f9a2b1e4d6c3a1f2e9b4d5c3a2f1e9b4d5022050..."
  }'
```

**Fetch a range of blocks:**
```bash
curl http://localhost:8081/blocks/range?from=0&to=5
```

**Trigger manual sync:**
```bash
curl "http://localhost:8081/sync?peer=http://localhost:8082"
```

---

## 2. Shared State and Race-Freedom (Locks vs Channels)

### 2.1 Architecture Decision: RWMutex (Not Channels)

**Why locks, not channels?**

The implementation uses **`sync.RWMutex`** to protect shared state, rather than Go's traditional channel-based concurrency model. This design choice is intentional:

1. **Concurrent Readers**: Multiple goroutines frequently read the blockchain simultaneously (HTTP handlers, gossip, sync). RWMutex allows readers to run in parallel without blocking each other.
   
2. **Write Rarity**: Writes (adding blocks/transactions) are infrequent compared to reads. Channels would serialize all access, unnecessarily throttling readers.
   
3. **Complex State Machine**: The blockchain has multiple interrelated pieces (blocks, pending transactions, fork tracking). A single RWMutex is simpler than coordinating multiple channels.
   
4. **Imperative API**: HTTP handlers naturally expect imperative access to state, not message passing. Channels would require building a separate RPC layer.

**Trade-off**: RWMutex is slightly less type-safe than channels, but provides better performance and simpler code for this workload.

### 2.2 Protected State

The `Node` struct protects all shared state with a single RWMutex:

```go
type Node struct {
    lock       sync.RWMutex
    
    // Blockchain data
    Blockchain *blockchain.Blockchain
    
    // Gossip deduplication
    seenTransactions map[string]struct{}    // txID -> seen
    seenBlocks       map[string]struct{}    // blockHash -> seen
    
    // Fork tracking
    forks            map[int]map[string]*blockchain.Block  // index -> (hash -> Block)
}
```

**Key invariants:**

- **All reads of `Blockchain`** must hold `lock.RLock()`, then defer `lock.RUnlock()`
- **All writes to `Blockchain`** must hold `lock.Lock()`, then defer `lock.Unlock()`
- **Writes always persist to disk** before releasing the lock (fail-fast on I/O errors)
- **Gossip operations** (block/transaction broadcast) happen **outside the lock** to avoid blocking other handlers

### 2.3 Deadlock Prevention

The design avoids deadlock through careful ordering:

1. **Never hold lock while calling external services**: 
   - Gossip is spawned as a goroutine with a local copy of peers
   - HTTP calls to other nodes happen outside the lock
   
2. **Lock release before I/O**: 
   - Blockchain state is updated while holding lock
   - But long operations (network calls) never hold lock

3. **No nested locks**: 
   - Only one mutex in the entire Node
   - No hierarchy or lock ordering issues

### 2.4 Critical Sections

#### 2.4.1 Reading State (e.g., HTTP GET /chain)

```go
func (n *Node) chainHandler(w http.ResponseWriter, r *http.Request) {
    n.lock.RLock()
    defer n.lock.RUnlock()
    
    writeJSON(w, n.Blockchain.Blocks)  // Safe: multiple readers OK
}
```

**Duration**: Minimal. JSON encoding happens after lock release (via defer).

#### 2.4.2 Adding a Transaction (POST /transactions)

```go
func (n *Node) handleIncomingTransaction(tx transaction.Transaction) (bool, error) {
    n.lock.Lock()
    defer n.lock.Unlock()
    
    // Step 1: Check for duplicate
    txID := transaction.TransactionID(tx)
    if _, ok := n.seenTransactions[txID]; ok {
        return false, nil  // Already seen
    }
    
    // Step 2: Validate and add to pending pool
    if err := n.Blockchain.AddTransaction(tx); err != nil {
        return false, err
    }
    
    // Step 3: Mark as seen
    n.seenTransactions[txID] = struct{}{}
    
    // Step 4: Persist
    if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
        return false, err
    }
    // Lock automatically released here via defer
}

// Step 5: Gossip happens OUTSIDE lock
go n.gossipTransaction(tx)
```

**Critical insight**: Gossip is decoupled from state updates. The sending node doesn't wait for peers to acknowledge—it sends asynchronously.

#### 2.4.3 Adding a Block (POST /blocks) - Linear Case

```go
func (n *Node) HandleIncomingBlock(block *blockchain.Block) (bool, error) {
    n.lock.Lock()
    defer n.lock.Unlock()
    
    // Check if this block extends the current chain (linear growth)
    currentHead := n.Blockchain.Blocks[len(n.Blockchain.Blocks)-1]
    isLinear := (block.PrevHash == currentHead.Hash && 
                 block.Index == currentHead.Index+1)
    
    if isLinear {
        // Fast path: block extends current chain
        if err := n.Blockchain.ValidateBlock(block); err != nil {
            return false, err
        }
        n.Blockchain.Blocks = append(n.Blockchain.Blocks, block)
        n.Blockchain.RemovePendingTransactions(block.Transactions)
        n.seenBlocks[block.Hash] = struct{}{}
        if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
            return false, err
        }
    }
}

// Gossip OUTSIDE lock
go n.gossipBlock(block)
```

#### 2.4.4 Fork Resolution (POST /blocks) - Non-Linear Case

When a block doesn't extend the current chain, the node stores it as a competing block and attempts fork resolution **all while holding the lock** (because it's modifying the fork tracking data structure and the blockchain):

```go
if !isLinear {
    // Store as competing block
    if n.forks[block.Index] == nil {
        n.forks[block.Index] = make(map[string]*blockchain.Block)
    }
    n.forks[block.Index][blockID] = block
    n.seenBlocks[blockID] = struct{}{}
    
    // Try to build a longer chain from competing blocks
    accepted, err := n.tryResolveForksLocked()
    if err != nil {
        return false, err
    }
}
```

### 2.5 Gossip Protocol

Gossip is **best-effort, asynchronous**, and decoupled from consensus:

```go
func (n *Node) gossipBlock(block *blockchain.Block) {
    peers := n.copyPeers()  // Snapshot peers while holding lock briefly
    
    for _, peer := range peers {
        client := &http.Client{Timeout: gossipTimeout}  // 2-second timeout
        url := strings.TrimRight(peer, "/") + "/blocks"
        
        // Try to send, but don't block or retry
        resp, err := client.Post(url, "application/json", payload)
        if err != nil {
            n.logger.Printf("gossip failed: %v", err)
            // Continue to next peer; no retry
        }
    }
}
```

**Guarantees:**
- Fire-and-forget: sender doesn't wait for peer acknowledgment
- No retry: single attempt per peer per block
- Timeout: 2 seconds, then move on
- Deduplication: seenBlocks/seenTransactions prevents processing duplicates

**Why this works:**
- Nodes sync periodically (auto-sync at startup, manual `/sync` endpoint)
- Longer chains are always preferred (longest-chain rule)
- Missing blocks will be fetched during the next sync cycle

---

## 3. Fork Resolution and Ledger Reorganization

### 3.1 Overview: The Longest-Chain Rule

When a node receives a block that doesn't extend its current chain, it:

1. **Validates** the block (proof-of-work, signature, etc.)
2. **Stores** it as a competing block in the fork tracking map
3. **Attempts** to build a complete competing chain from stored blocks
4. **Compares** competing chain to current chain
5. **Reorganizes** (reorg) if the competing chain is longer and valid

### 3.2 Fork Tracking Data Structure

```go
forks map[int]map[string]*blockchain.Block
// forks[3]["0000abc..."] = &Block{Index: 3, Hash: "0000abc...", ...}
// forks[3]["0000def..."] = &Block{Index: 3, Hash: "0000def...", ...}  // Competing
// forks[4]["0000xyz..."] = &Block{Index: 4, Hash: "0000xyz...", ...}
```

**Purpose:** Stores blocks received out of order. When a block arrives at height 5 before height 4, we store it. Later, when height 4 arrives, we can now build a chain from heights 4→5→...

**Lifetime:** Entries are deleted after reorganization to avoid stale forks.

### 3.3 Competing Chain Construction: BuildCandidateChain

When a fork block arrives, the node tries to build the longest possible competing chain:

```go
func (n *Node) BuildCandidateChain(
    startBlock *blockchain.Block, 
    competingBlocks map[int]map[string]*blockchain.Block,
) []*blockchain.Block {
    chain := []*blockchain.Block{startBlock}
    currentBlock := startBlock
    
    // Greedily extend forward
    for nextIdx := currentBlock.Index + 1; nextIdx < 1000; nextIdx++ {
        candidates, ok := competingBlocks[nextIdx]
        if !ok || len(candidates) == 0 {
            break  // No more blocks available
        }
        
        // Find a block that links to current block
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
            break  // Chain broken; can't continue
        }
    }
    
    return chain
}
```

**Example:**
- Current chain: `[Genesis(#0), Block#1, Block#2]` (length 3)
- Fork storage: `forks[3] = {BlockA}`, `forks[4] = {BlockB}`, `forks[5] = {BlockC}`
- BlockB.PrevHash = BlockA.Hash, BlockC.PrevHash = BlockB.Hash
- Result: Candidate chain `[BlockA, BlockB, BlockC]` (length 3)
- Comparison: 3 + 3 (fork start) = 6 vs 3 (current) → **Reorg!**

### 3.4 Competing Chain Validation: validateCandidateChainLocked

Before accepting a competing chain, the node validates it **in isolation**:

```go
func (n *Node) validateCandidateChainLocked(candidate []*blockchain.Block) error {
    forkStart := candidate[0].Index
    
    // Create a clone of the blockchain truncated at fork start
    clone := blockchain.CopyBlockchain(n.Blockchain)
    clone.Blocks = clone.Blocks[:forkStart]  // Keep only pre-fork blocks
    
    // Validate each candidate block against this clone state
    for _, block := range candidate {
        if err := clone.ValidateBlock(block); err != nil {
            return err  // Block validation failed
        }
        clone.Blocks = append(clone.Blocks, block)
    }
    
    return nil  // All blocks valid
}
```

**What is validated:**
- Proof-of-work (hash has required leading zeros)
- Merkle root (tree of transaction hashes is correct)
- Transaction signatures (each tx is signed by sender)
- Balances (sender has sufficient funds after previous blocks in chain)
- No double-spending (within the candidate chain)

**Key detail**: Validation happens on a **clone**, not the real blockchain, so a failed validation doesn't corrupt state.

### 3.5 Chain Reorganization: reorganizeChainLocked

If a competing chain is longer and valid, the node executes a reorganization:

```go
func (n *Node) reorganizeChainLocked(candidate []*blockchain.Block) error {
    forkStart := candidate[0].Index
    oldHeight := len(n.Blockchain.Blocks) - 1
    
    // Phase 4.4a: Identify orphaned blocks
    orphanedBlocks := n.Blockchain.Blocks[forkStart:]
    n.logger.Printf("reorg: %d orphaned blocks at heights %d-%d", 
        len(orphanedBlocks), forkStart, oldHeight)
    
    // Phase 4.4b: Replace main chain
    newBlocks := n.Blockchain.Blocks[:forkStart]
    newBlocks = append(newBlocks, candidate...)
    n.Blockchain.Blocks = newBlocks
    
    // Phase 4.4c: Mark new blocks as seen
    for _, block := range candidate {
        n.Blockchain.RemovePendingTransactions(block.Transactions)
        n.seenBlocks[block.Hash] = struct{}{}
    }
    
    // Phase 4.4d: Extract transactions from orphaned blocks
    var orphanedTxs []transaction.Transaction
    for _, orphanBlock := range orphanedBlocks {
        if orphanBlock != nil {
            orphanedTxs = append(orphanedTxs, orphanBlock.Transactions...)
        }
    }
    
    // Phase 4.4e: Rebuild ledger and re-validate orphaned transactions
    validatedOrphanedTxs := []transaction.Transaction{}
    for _, tx := range orphanedTxs {
        txID := transaction.TransactionID(tx)
        
        // Skip if already in new chain
        alreadyInChain := false
        for _, block := range candidate {
            for _, blockTx := range block.Transactions {
                if transaction.TransactionID(blockTx) == txID {
                    alreadyInChain = true
                    break
                }
            }
            if alreadyInChain {
                break
            }
        }
        if alreadyInChain {
            continue
        }
        
        // Re-validate against new balances
        if err := n.Blockchain.AddTransaction(tx); err != nil {
            n.logger.Printf("reorg: orphaned tx rejected: %v", err)
            continue  // Dropped; was valid in old chain but not new one
        }
        validatedOrphanedTxs = append(validatedOrphanedTxs, tx)
    }
    
    // Phase 4.4f: Persist to disk
    if err := n.Blockchain.SaveToFile(n.Config.DataFile); err != nil {
        return err
    }
    
    // Phase 4.4g: Clean up fork storage
    for idx := forkStart; idx < len(n.Blockchain.Blocks); idx++ {
        delete(n.forks, idx)
    }
    
    return nil
}
```

### 3.6 Ledger Rebuild During Reorg: The Critical Step

**Step 4.4e** is the heart of reorganization. Here's why it matters:

#### The Problem

When a chain reorganizes, account balances change. Consider:

**Old chain:**
```
Block#1: Alice → Bob (50)     [Alice: 200-50=150, Bob: 0+50=50]
Block#2: Bob → Charlie (20)   [Alice: 150, Bob: 50-20=30, Charlie: 0+20=20]
```

**New chain (competing):**
```
Block#1: Bob → Charlie (40)   [Alice: 200, Bob: 0-40=FAIL (insufficient)!]
```

The transaction `Bob → Charlie (40)` is only valid if Bob received funds first. In the old chain, he did (from Alice). In the new chain, he didn't—so this tx is invalid.

**Ledger Rebuild Process:**

1. **Truncate state at fork point**: Reset blockchain to common ancestor
2. **Reapply new chain's blocks**: Rebuild balances from new chain
3. **Re-validate orphaned transactions**: Try to add them back using new balances
4. **Accept/drop based on new ledger state**: If balance check fails, tx is dropped

**Outcome:**
- Some orphaned txs are restored to mempool (became valid again in new ledger)
- Some orphaned txs are dropped (became invalid in new ledger)
- The ledger is consistent with the new canonical chain

#### Example Scenario

**Original state (balances post-mining):**
```
Alice: 200  Bob: 0  Charlie: 0
```

**Old chain (what node thought was canonical):**
```
Block#1: Alice → Bob (100)
  → Balances: Alice: 100, Bob: 100, Charlie: 0
Block#2: Bob → Charlie (50)
  → Balances: Alice: 100, Bob: 50, Charlie: 50
```

**Competing chain (received out of order, now looks longer):**
```
Block#1: Alice → Charlie (120)
  → Balances: Alice: 80, Bob: 0, Charlie: 120
Block#2: Alice → Bob (60)
  → Balances: Alice: 20, Bob: 60, Charlie: 120
```

**Reorganization:**
1. Old chain's Block#2 `(Bob → Charlie 50)` becomes orphaned
2. Node rebuilds state from common ancestor (Genesis)
3. Applies new chain's blocks: Alice: 20, Bob: 60, Charlie: 120
4. Tries to re-add orphaned tx `(Bob → Charlie 50)`
   - Bob has 60, tx asks for 50 → **Valid!**
5. Restored to mempool: `Bob → Charlie (50)`

**What if it were invalid?**
- Suppose orphaned tx was `Bob → Dave (100)` instead
- Bob only has 60 in new ledger → **Rejected!**
- Tx is permanently dropped from this node's view

### 3.7 Synchronization: Competing Chains Across Network

Reorganization also happens during manual peer sync:

```go
func (n *Node) SyncFromPeer(peerURL string) error {
    // Step 1: Get peer's chain height
    peerInfo, _ := n.fetchChainInfo(peerURL)
    peerHeight := peerInfo["height"]
    
    // Step 2: Compare with local
    localHeight := len(n.Blockchain.Blocks) - 1
    if peerHeight <= localHeight {
        return nil  // Peer not longer; no sync
    }
    
    // Step 3: Download missing blocks
    blocks, _ := n.fetchBlockRange(peerURL, localHeight+1, peerHeight)
    
    // Step 4: Apply blocks (triggers reorg if fork detected)
    return n.ApplySyncedBlocks(blocks)
}
```

**ApplySyncedBlocks** handles both linear growth (blocks extend current chain) and forks (blocks from competing chain arrive in sync response):

```go
func (n *Node) ApplySyncedBlocks(blocks []*blockchain.Block) error {
    n.lock.Lock()
    defer n.lock.Unlock()
    
    for i, block := range blocks {
        if err := n.Blockchain.ValidateBlock(block); err != nil {
            return err  // Stop at first invalid block
        }
        
        // Append block and update state
        n.Blockchain.Blocks = append(n.Blockchain.Blocks, block)
        n.Blockchain.RemovePendingTransactions(block.Transactions)
        n.seenBlocks[block.Hash] = struct{}{}
    }
    
    // Persist
    return n.Blockchain.SaveToFile(n.Config.DataFile)
}
```

**Note**: Sync only handles linear chains. True fork resolution (competing chains) happens via the gossip protocol (`/blocks` endpoint), where `HandleIncomingBlock` triggers `tryResolveForksLocked`.

---

## 4. Complete Example: A Fork and Resolution

### 4.1 Initial State

Two nodes, A and B, both with identical chains:
```
A: [Genesis, Block#1, Block#2]
B: [Genesis, Block#1, Block#2]
```

Both have common balances: Alice=100, Bob=50, Charlie=20.

### 4.2 Network Partition

Nodes are isolated. Each mines a competing Block#3:

**Node A mines:**
```
Block#3: Alice → Bob (30)
Hash: 0000xyzabc...
A's chain: [Genesis, Block#1, Block#2, Block#3(new)]
A's balances: Alice=70, Bob=80, Charlie=20
```

**Node B mines (simultaneously):**
```
Block#3: Alice → Charlie (40)
Hash: 0000defghi...
B's chain: [Genesis, Block#1, Block#2, Block#3(new)]
B's balances: Alice=60, Bob=50, Charlie=60
```

### 4.3 Reconnection

Network heals. Node A receives B's Block#3 (different hash, same index 3):

**At Node A:**
```
1. POST /blocks with B's Block#3
2. HandleIncomingBlock receives it
3. Check if linear: B's #3.PrevHash == A's #2.Hash? YES!
   But wait—A already has a #3 (the one it mined)
   Actually: B's #3 doesn't extend A's chain (A's #2→A's #3 done)
   B's #3.PrevHash points to A's #2, but A has different #3
   → NOT LINEAR (block index gap or hash mismatch)
4. Store in forks[3]["0000defghi..."] = B's Block#3
5. Try to resolve forks:
   - Current chain length: 4 (indices 0-3)
   - Candidate starting at #3 has length 1
   - Candidate total length: 3 + 1 = 4
   - Current length: 4
   - NOT LONGER → No reorganization
```

**Result**: Both blocks exist; B's is stored as fork, A's remains canonical.

### 4.4 Further Mining

Node B mines a Block#4 while still offline:

**Node B's state:**
```
[Genesis, Block#1, Block#2, Block#3_B, Block#4_B]
Length: 5
```

Later, A and B reconnect. B's Block#4 reaches A:

**At Node A:**
```
1. POST /blocks with B's Block#4
2. B's #4.PrevHash == B's #3.Hash
3. B's #3 is in forks[3], B's #4 has matching PrevHash
4. BuildCandidateChain starting from B's #3:
   - Finds B's #4 at index 4 (in forks[4])
   - Chain: [B's #3, B's #4]
5. Candidate total length: 3 + 2 = 5
6. Current length: 4
7. 5 > 4 → LONGER!
8. Validate candidate chain (both blocks valid, balances OK)
9. **REORGANIZE!**
```

### 4.5 Reorganization at Node A

```
Old chain:  [Genesis, #1, #2, A's#3]
New chain:  [Genesis, #1, #2, B's#3, B's#4]

Orphaned:   [A's#3]
Orphaned txs: [Alice → Bob (30)]

New balances:
- Apply Genesis: Alice=100, Bob=0, Charlie=0
- Apply #1: (same as before) Alice=???, Bob=???, Charlie=???
- Apply #2: (same as before) balances=???
- Apply B's #3: Alice → Charlie (40)
  → Alice=60, Bob=50, Charlie=40
- Apply B's #4: (new txs) balances=???

Try to restore orphaned tx: Alice → Bob (30)
- Alice has 60 (after B's blocks applied)
- Tx needs 30
- **Valid!** → Add to mempool

Final state at A:
- Canonical chain: [Genesis, #1, #2, B's#3, B's#4]
- Mempool: [Alice → Bob (30)] (waiting to be mined)
- Balances match B's: Alice=60, Bob=50, Charlie=40
```

### 4.6 Ledger Report

After reorg completes, A logs:
```
[node] reorg complete: fork_start=3 old_height=3 new_height=4 orphaned=1 restored_txs=1
```

This tells us:
- Fork began at height 3
- Old chain height was 3, new height is 4
- 1 block was orphaned (A's original #3)
- 1 orphaned transaction was restored (valid in new ledger)

---

## 5. Concurrency Guarantees

### 5.1 Safety Properties

1. **Consistency**: The blockchain is always in a valid state (no partial writes)
   - Blocks are validated before adding
   - Balances are recomputed during reorg
   - Disk persists after every state change

2. **Isolation**: Concurrent readers don't interfere
   - RWMutex allows multiple readers
   - Writers wait for all readers (and vice versa)

3. **Atomicity**: State transitions are atomic with respect to disk
   - Lock held during validation + update + persist
   - If persist fails, state is rolled back (error returned)

4. **Durability**: Blockchain persists to disk before releasing lock
   - Node crashes don't lose canonical chain
   - Gossip queue is cleared (no persistence needed—nodes sync on restart)

### 5.2 Liveness Properties

1. **No deadlock**: Single lock, no circular waits
2. **No starvation**: RWMutex is fair; no goroutine waits forever
3. **No livelock**: Fork resolution doesn't loop; each reorg finalizes state
4. **Resilience to delays**: Gossip timeout (2s) prevents hanging; HTTP endpoints respond promptly

---

## 6. Summary Table

| Aspect | Design | Rationale |
|--------|--------|-----------|
| **Serialization** | JSON over HTTP/REST | Platform-neutral, easy to inspect, standard for web services |
| **State Protection** | Single RWMutex | Prevents race conditions; RWMutex allows concurrent readers |
| **Gossip Protocol** | Best-effort, async, fire-and-forget | Avoids blocking; peers sync on reconnect |
| **Fork Detection** | Stored in `forks` map, indexed by height | Enables out-of-order block handling |
| **Competing Chains** | Built greedily, validated on clone | Safe validation; no mutation of real state |
| **Reorganization** | 4-phase (identify orphans, replace chain, extract txs, rebuild ledger) | Clear steps, explicit logging, ledger consistency |
| **Ledger Rebuild** | Re-validate orphaned txs against new balances | Ensures consistency across fork point |
| **Synchronization** | Peer pulls longer chain via `/chain/info` + `/blocks/range` | Simple, doesn't require push; resilient to peer failure |

