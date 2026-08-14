# Toy Blockchain – Multi-Node Network

A robust, networked toy blockchain implementation written in Go, featuring proof-of-work consensus, Ed25519 cryptographic signatures, HTTP REST APIs, peer-to-peer gossip networking, chain synchronization, longest-chain fork resolution, and chain reorganization.

---

## Overview

This project extends a single-node Proof-of-Work (PoW) blockchain into a fully functional multi-node distributed ledger network. Nodes run independently, communicate over HTTP REST APIs, broadcast transactions and blocks via gossip protocol, handle out-of-order/competing blocks, and automatically resolve forks by reorganizing state to adopt the longest valid chain.

---

## Architecture

The system follows a modular architecture separating core data structures, cryptographic operations, networking protocols, and command-line execution:

```
                  ┌──────────────────────────────────────────────┐
                  │                 cmd/main.go                  │
                  │           (CLI Command Dispatcher)           │
                  └──────────────────────┬───────────────────────┘
                                         │
                 ┌───────────────────────┴───────────────────────┐
                 │                                               │
                 ▼                                               ▼
   ┌───────────────────────────┐                   ┌───────────────────────────┐
   │    blockchain/ package    │                   │       node/ package       │
   ├───────────────────────────┤                   ├───────────────────────────┤
   │ - Block & Blockchain      │                   │ - HTTP REST Server        │
   │ - Proof-of-Work Mining    │◄──────────────────│ - P2P Gossip Broadcast    │
   │ - SHA-256 Hashing         │   (Uses core data │ - Chain Synchronization   │
   │ - Ed25519 Verification    │    & validation)  │ - Fork Storage & Reorg    │
   │ - Ledger Persistence      │                   │ - RWMutex Concurrency     │
   └───────────────────────────┘                   └───────────────────────────┘
```

---

## Features

### Round 1 Features
* **Deterministic Genesis Block**: Pre-configured genesis block ensures all fresh nodes start with identical state.
* **Proof-of-Work (PoW) Mining**: SHA-256 hash puzzle requiring configurable leading zeros (`--difficulty`).
* **JSON File Persistence**: Local storage of blockchain and pending transactions to disk.
* **Transaction Pool (Mempool)**: Unmined pending transaction queue with balance checking.
* **Integrity Validation**: Complete verification of hashes, index sequence, and previous hash linkage.

### Phase 1 – Refactoring and Ed25519
* **Reusable Modular Packages**: Clean separation into `blockchain`, `transaction`, `node`, and `cmd` packages.
* **Ed25519 Cryptographic Signatures**: Non-faucet (`non-SYSTEM`) transactions require Ed25519 private key signatures.
* **PEM Key Management**: CLI tools to generate `.pem` key pairs and verify sender identity.

### Phase 2 – HTTP Node
* **Standalone HTTP Server**: Each node runs an independent HTTP REST server (`net/http`).
* **State Introspection**: Endpoints for checking health, node status, connected peers, mempool, and balances.

### Phase 3 – Gossip
* **Transaction Gossip**: Incoming valid transactions are automatically gossiped to all configured peers.
* **Block Gossip**: Mined or received valid blocks are immediately propagated across the network.
* **De-duplication**: In-memory `seenTransactions` and `seenBlocks` maps prevent infinite gossip loops.

### Phase 4 – Synchronization and Fork Resolution
* **Automatic & On-Demand Sync**: Fresh or lagging nodes query peers via `/chain/info` and download missing blocks via `/blocks/range`.
* **Longest-Chain Fork Resolution**: Nodes store competing valid blocks in a fork tree and adopt the chain with the most cumulative blocks.
* **Chain Reorganization**: When a longer valid chain is discovered, the node rewinds to the common ancestor, adopts the winning blocks, extracts transactions from orphaned blocks, validates them against the new ledger, and restores valid ones to the mempool.
* **Thread Safety**: Complete concurrent access protection using `sync.RWMutex`.

---

## Project Structure

```
toy-blockchain/
├── blockchain/
│   ├── block.go          # Block structure, Mining logic, and Merkle tree root calculation
│   ├── blockchain.go     # Blockchain state, ledger accounting, validation, and JSON I/O
│   ├── fork.go           # Deep-copy utility (CopyBlockchain) and static ResolveFork policy
│   └── hash.go           # SHA-256 block hashing utilities
├── transaction/
│   └── transaction.go    # Ed25519 key generation, signing, signature verification, & ID generation
├── node/
│   ├── node.go           # HTTP REST server, P2P gossip, auto-sync, fork tree, & reorg engine
│   ├── node_test.go      # HTTP API unit tests
│   └── gossip_test.go    # P2P gossip, transaction broadcast, & block propagation tests
├── cmd/
│   └── main.go           # CLI application entry point and flag parsing
├── tests/                # Integration tests (convergence, sync, signatures, mining, etc.)
├── gossip_demo.ps1       # PowerShell multi-node cluster demo script
├── fork_demo.ps1         # PowerShell fork resolution & reorganization demo script
├── go.mod                # Go module definition
└── README_V2.md          # Comprehensive documentation
```

---

## Requirements

* **Go**: Version 1.20 or later installed and available in `PATH`.
* **OS**: Cross-platform (Windows, Linux, macOS).

---

## Configuration

The CLI supports global runtime flags:

| Flag | Format | Default | Description |
| :--- | :--- | :--- | :--- |
| `--listen` / `--address` | `--listen :8081` | `""` | HTTP listen address for `run-node` |
| `--peer` | `--peer http://localhost:8082` | `[]` | Peer node URL (can be specified multiple times) |
| `--file` | `--file node1.json` | `blockchain.json` | Path to JSON file used for state persistence |
| `--difficulty` | `--difficulty 4` | `4` | Mining difficulty (number of leading hex zeros required) |
| `--blocksize` | `--blocksize 10` | `10` | Maximum number of transactions per mined block |
| `--key` | `--key alice.pem` | `""` | PEM key file used to sign `addtx` CLI transactions |

---

## Running a Single Node

To initialize and run a single node:

```bash
# 1. Initialize local blockchain file
go run ./cmd init --file node1.json

# 2. Start the HTTP node service
go run ./cmd run-node --listen :8081 --file node1.json
```

---

## Running a Local Cluster

To run a 3-node local cluster (Node A, Node B, Node C) communicating over HTTP:

### Terminal 1 – Node A (Port 8081)
```bash
go run ./cmd init --file node1.json
go run ./cmd run-node --listen :8081 --file node1.json
```

### Terminal 2 – Node B (Port 8082, peering with Node A)
```bash
go run ./cmd init --file node2.json
go run ./cmd run-node --listen :8082 --file node2.json --peer http://localhost:8081
```

### Terminal 3 – Node C (Port 8083, peering with Node A & B)
```bash
go run ./cmd init --file node3.json
go run ./cmd run-node --listen :8083 --file node3.json --peer http://localhost:8081 --peer http://localhost:8082
```

---

## HTTP API

All endpoints communicate using standard JSON payloads over HTTP.

### Endpoint Overview

| Method | Endpoint | Purpose |
| :--- | :--- | :--- |
| `GET` | `/health` | Node health status check |
| `GET` | `/status` | General node status, configuration, & chain length |
| `GET` | `/peers` | List configured peer node URLs |
| `GET` | `/chain` | Fetch entire blockchain array |
| `GET` | `/chain/info` | Fetch chain height & head hash (used for sync) |
| `GET` | `/block/{index}` | Fetch specific block by height |
| `GET` | `/blocks/range?from=X&to=Y` | Fetch array of blocks between index X and Y (inclusive) |
| `GET` | `/mempool` | View unmined pending transactions |
| `GET` | `/balances` | View current calculated ledger account balances |
| `POST` | `/transactions` | Submit a signed transaction (triggers gossip) |
| `POST` | `/blocks` | Submit/propagate a mined block (triggers fork checks & gossip) |
| `POST` | `/mine` | Mine pending mempool transactions into a new block |
| `GET` | `/sync?peer=<url>` | Manually trigger chain synchronization from a peer |

---

### API Examples

> **Note for Windows PowerShell Users:**  
> In Windows PowerShell, `curl` is an alias for `Invoke-WebRequest`, which does not accept Linux flags (`-X`, `-H`, `-d`) or backslash `\` line continuations.  
> Use `curl.exe` directly or use native PowerShell `Invoke-RestMethod` shown below.

#### 1. Health Check
* **cURL (Bash / CMD / PowerShell with `curl.exe`):**
  ```bash
  curl http://localhost:8081/health
  ```
* **PowerShell:**
  ```powershell
  Invoke-RestMethod -Uri http://localhost:8081/health
  ```
**Response (200 OK):**
```
OK
```

#### 2. Get Node Status
* **cURL:**
  ```bash
  curl http://localhost:8081/status
  ```
* **PowerShell:**
  ```powershell
  Invoke-RestMethod -Uri http://localhost:8081/status
  ```
**Response (200 OK):**
```json
{
  "block_size": 10,
  "chain_length": 1,
  "data_file": "node1.json",
  "difficulty": 4,
  "peers": [
    "http://localhost:8082"
  ]
}
```

#### 3. View Ledger Balances
* **cURL:**
  ```bash
  curl http://localhost:8081/balances
  ```
* **PowerShell:**
  ```powershell
  Invoke-RestMethod -Uri http://localhost:8081/balances
  ```
**Response (200 OK):**
```json
{
  "Alice": 100,
  "Bob": 50
}
```

#### 4. Submit Signed Transaction (Automated PEM Signing & HTTP Posting)

You can automatically extract keys from `alice.pem`, sign the transaction, and post it to a live running HTTP node in a single command using `--url` (or `--node`):

* **Automated CLI Command (Loads `alice.pem`, signs payload, & posts to live HTTP node):**
  ```powershell
  go run ./cmd addtx Alice Bob 10 --key alice.pem --url http://localhost:8081
  ```

* **What happens automatically under the hood**:
  1. Reads `alice.pem` private key directly from disk.
  2. Extracts Ed25519 `publicKey`.
  3. Computes `signature` over `Alice|Bob|10.00000000`.
  4. Posts the signed JSON payload to `http://localhost:8081/transactions`.
  5. The receiving node validates the signature and gossips the transaction across all connected peers!

---

* **Manual `Invoke-RestMethod` Example (If crafting raw JSON manually):**
  ```powershell
  Invoke-RestMethod -Method Post -Uri http://localhost:8081/transactions `
    -ContentType "application/json" `
    -Body '{
      "sender": "Alice",
      "recipient": "Bob",
      "amount": 10,
      "publicKey": "5fa4089ab30f8c3366e6f3267874e365847378772dceb7a04e055082480ec54f",
      "signature": "a4a7325a1e01285d7b563e1fe866084c6302e1e5fdef42127d15601cd18a30ab50eb7a12974f24bb13976164c8b2a9510e9981f920614787702146e85d9d6606"
    }'
  ```
**Response (202 Accepted):**
```json
{
  "status": "accepted"
}
```

#### 5. Mine Pending Transactions
* **cURL:**
  ```bash
  curl.exe -X POST http://localhost:8081/mine
  ```
* **PowerShell:**
  ```powershell
  Invoke-RestMethod -Method Post -Uri http://localhost:8081/mine
  ```
**Response (200 OK):**
```json
{
  "hash": "0000a1b2c3d4e5...",
  "index": 1,
  "status": "mined"
}
```

#### 6. Trigger Chain Sync
* **cURL:**
  ```bash
  curl "http://localhost:8081/sync?peer=http://localhost:8082"
  ```
* **PowerShell:**
  ```powershell
  Invoke-RestMethod -Uri "http://localhost:8081/sync?peer=http://localhost:8082"
  ```
**Response (200 OK):**
```json
{
  "peer": "http://localhost:8082",
  "status": "synced"
}
```

---

## Transaction and Block Validation

Every block and transaction added locally or received from peers undergoes rigorous multi-stage validation:

### Transaction Validation Rules
1. **Amount Verification**: Amount must be strictly greater than 0.
2. **Signature Verification**: Non-`SYSTEM` transactions must provide a valid Ed25519 `PublicKey` and `Signature` matching the transaction payload (`sender|recipient|amount`).
3. **Balance Verification**: The sender's confirmed balance (including pending spends) must be sufficient.

### Block Validation Rules
1. **Hash Integrity**: Re-calculated SHA-256 hash must equal `block.Hash`.
2. **Merkle Root**: Re-computed Merkle root of block transactions must match `block.MerkleRoot`.
3. **Proof-of-Work**: `block.Hash` must start with `Difficulty` number of leading zero characters.
4. **Sequence & Linkage**: `block.Index` must equal `prevBlock.Index + 1` and `block.PrevHash` must equal `prevBlock.Hash`.

---

## Chain Synchronization

When a node starts up or falls behind:
1. **Auto-Sync on Startup**: Node sends `GET /chain/info` to configured peers.
2. **Height Comparison**: If a peer reports a higher chain tip (`peerHeight > localHeight`), the node requests missing blocks via `GET /blocks/range?from=localHeight+1&to=peerHeight`.
3. **Sequential Apply**: Downloaded blocks are validated sequentially against the chain tip and appended using `ApplySyncedBlocks()`.

---

## Fork Resolution and Reorganization

When concurrent block mining creates competing valid branches:

```
Genesis ─── Block 1 ─── Block 2 (Main Chain - Length 3)
                           └────── Block 2' ─── Block 3' (Fork Chain - Length 4 - WINNER)
```

1. **Fork Storage**: Valid blocks that do not extend the current tip are saved in `n.forks[block.Index]`.
2. **Candidate Assembly**: [`tryResolveForksLocked()`](file:///c:/Users/nimes/Desktop/Bot%20Calm/Blockchain%20Assesment%202/toy-blockchain/node/node.go#L365) builds alternative candidate chains.
3. **Trial Validation**: The candidate chain is evaluated against a temporary clone of the chain created via [`blockchain.CopyBlockchain()`](file:///c:/Users/nimes/Desktop/Bot%20Calm/Blockchain%20Assesment%202/toy-blockchain/blockchain/fork.go#L13-L57).
4. **Longest-Chain Adoption**: If a candidate chain has greater cumulative length than the local chain, [`reorganizeChainLocked()`](file:///c:/Users/nimes/Desktop/Bot%20Calm/Blockchain%20Assesment%202/toy-blockchain/node/node.go#L430) triggers:
   * **Orphan Extraction**: Blocks on the abandoned branch are identified as orphaned.
   * **State Rewind**: The main chain is rewound to the common fork point (`forkStart`).
   * **Chain Switch**: The winning candidate blocks are appended.
   * **Mempool Recovery**: Transactions from orphaned blocks are validated against the new balances; valid ones are restored to `PendingTransactions`.

---

## Concurrency and Race Safety

The node architecture is engineered for multi-threaded safety using Go's standard synchronization primitives:

* **State Locking**: All reads and writes to shared node state (`n.Blockchain`, `n.seenTransactions`, `n.seenBlocks`, `n.forks`) are guarded by `n.lock` (`sync.RWMutex`).
* **Non-Blocking Gossip**: Outbound HTTP requests for transaction/block gossip are executed in separate background goroutines (`go n.gossipBlock(block)`) to prevent blocking HTTP handler threads.
* **Safe State Copying**: During candidate chain validation and gossip peer iterations, defensive deep-copies (`CopyBlockchain` and `copyPeers`) are created to prevent data races.

---

## Testing

The project includes unit, integration, and stress tests:

```bash
# Run all node unit tests
go test ./node -v

# Run full integration test suite
go test ./tests -v
```

### Key Test Suites
* `node/node_test.go`: Tests HTTP REST endpoints.
* `node/gossip_test.go`: Tests P2P transaction gossip and 3-node block broadcasting.
* `tests/signature_test.go`: Tests Ed25519 key generation, signing, and signature verification.
* `tests/sync_test.go`: Tests chain sync and reorganization.
* `tests/convergence_test.go`: Tests multi-node cluster convergence under competing block generation.

---

## Example Multi-Node Scenario

You can run the interactive PowerShell demonstration script to observe 3 nodes syncing and gossiping in real-time:

```powershell
.\gossip_demo.ps1
```

Or run the fork simulation script to demonstrate chain reorganization:

```powershell
.\fork_demo.ps1
```

---

## Round 1 vs Current Version

| Feature | Round 1 | Current Version |
| :--- | :--- | :--- |
| **Architecture** | Single local binary process | Distributed multi-node network |
| **Authentication** | Balance-only checks | Ed25519 cryptographic signatures |
| **Interface** | Local CLI commands | HTTP REST API + CLI client |
| **Network Topology** | Isolated local state | Peer-to-peer gossip network |
| **De-duplication** | None | Hash-based message de-duplication |
| **Synchronization** | None | Automatic height-range sync |
| **Fork Handling** | Simple offline simulation | Dynamic fork tree + online chain reorg |
| **Concurrency** | Single-threaded CLI | Race-free concurrent `RWMutex` node server |

---

## Design Decisions

1. **Ed25519 Signatures**: Selected Ed25519 (NIST Curve25519) for fast signature verification and compact key sizes.
2. **HTTP REST Protocols**: Used Go's native `net/http` stack for clear inspection, easy debugging, and straightforward curl/script automation.
3. **Longest-Valid-Chain Consensus**: Standard PoW rule prioritizing cumulative chain length, ensuring eventual consistency across peers.
4. **Orphaned Transaction Restoration**: Reorgs do not drop transactions from abandoned blocks; valid non-conflicting transactions are safely returned to the mempool.
5. **In-Memory De-duplication**: `seenTransactions` and `seenBlocks` maps enforce idempotent message handling across gossip peers.

---

## Limitations

* **Fixed Difficulty**: Difficulty retargeting algorithm is supported per block, but default difficulty is fixed for quick testing.
* **HTTP Polling/Gossip**: Uses HTTP POST gossip rather than persistent WebSockets or gRPC streams.
* **In-Memory Fork Storage**: `n.forks` tree is stored in memory and cleared on node process restart.

---

## Future Improvements

* **WebSocket P2P Transport**: Upgrade HTTP POST gossip to bi-directional WebSocket connections.
* **Persistent Fork DB**: Persist competing block branches to disk using embedded KV databases (e.g. Pebble / BoltDB).
* **UTXO Model**: Upgrade account balance accounting to a UTXO (Unspent Transaction Output) model.
