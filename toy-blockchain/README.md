# 🔗 Toy Blockchain — Built in Go

A small Go-based toy blockchain implementation with persistence, mining, transaction validation, and blockchain integrity checks.

## Build & Run

From `toy-blockchain/`:

```bash
go build ./cmd
```

Then run the CLI from the workspace root:

```bash
./cmd.exe init
./cmd.exe addtx SYSTEM Alice 50
./cmd.exe mine
./cmd.exe print
./cmd.exe balances
./cmd.exe validate
./cmd generate-key alice
```

Or skip the build step and run directly with Go:

```bash
go run ./cmd init
```

## Tests

Run the unit tests with:

```bash
go test ./tests
```

## Command-Line Commands

The CLI supports these commands:

- `go run ./cmd init`
  - Create or reinitialize the local blockchain and save it to `blockchain.json`
- `go run ./cmd addtx <sender> <recipient> <amount>`
  - Add a transaction to the pending pool
  - `SYSTEM` is used as a faucet account for initial balance injection
- `go run ./cmd generate-key alice`
  - generate a new PEM key file automatically
- `go run ./cmd mine`
  - Mine a new block from pending transactions and append it to the chain
- `go run ./cmd print`
  - Print the full blockchain contents to stdout
- `go run ./cmd balances`
- `go run ./cmd balance <acc>`
  - Show current confirmed account balances
- `go run ./cmd validate`
  - Validate blockchain integrity and proof-of-work
- `go run ./cmd simulate-fork`
  - Simulate a local fork with two independent chain copies, competing blocks, and longest-chain resolution
- `go run ./cmd run-node --listen :8081 --peer http://localhost:8082 --file node1.json`
  - Start the node HTTP service for introspection and status

## Runtime Configuration

The CLI supports optional flags for mining and persistence:

```bash
go run ./cmd mine --difficulty=5 --blocksize=2 --file=mychain.json
```

- `--difficulty` sets proof-of-work difficulty (default: `4`)
- `--blocksize` limits how many pending transactions are included in the mined block (default: `10`)
- `--file` chooses the JSON file used for blockchain persistence (default: `blockchain.json`)
- `--listen` / `--address` sets the HTTP listen address for `run-node`
- `--peer` registers a peer URL for future node operations

## Example: Initialize a node and create a block

This example creates a chain file, adds transactions, mines a block, starts the node HTTP service and synchronization.

```bash
go run ./cmd init --file node1.json

go run ./cmd generate-key alice

go run ./cmd addtx SYSTEM Alice 50 --file node1.json

go run ./cmd addtx Alice Bob 20 --key alice.pem --file node1.json

go run ./cmd mine --file node1.json

go run ./cmd run-node --listen :8081 --peer http://localhost:8082 --file node1.json

curl "http://localhost:8081/sync?peer=http://localhost:8082"
```

Once the node is running, you can inspect it at:

```bash
curl http://localhost:8081/health
curl http://localhost:8081/status
curl http://localhost:8081/chain
curl http://localhost:8081/block/0
curl http://localhost:8081/mempool
curl http://localhost:8081/balances
```

## New Features

- `simulate-fork` simulates a fork between two independent copies of the local blockchain and resolves the fork using the longest valid chain.
- Difficulty retargeting is now supported: each mined block stores the difficulty used, mining time is measured, and the network difficulty is adjusted for the next block.

## Project Layout

- `cmd/main.go`
  - CLI entry point and command dispatch
- `blockchain/block.go`
  - Block structure, constructor, and mining logic
- `blockchain/hash.go`
  - SHA-256 block hashing, transaction serialization, and hash contract
- `blockchain/blockchain.go`
  - Blockchain state, genesis creation, transaction validation, mining, persistence, and validation logic
- `tests/`
  - Unit tests for hashing, block construction, mining, and chain validation

## Design Decisions

- **Deterministic Genesis block**: Genesis is created with fixed values (`Index=0`, `Timestamp=0`, `PrevHash=64 zeros`, `Nonce=0`) so every fresh node starts from the same root.
- **SHA-256 hashing contract**: The block hash is computed from the exact serialization order `Index | Timestamp | Transactions | PrevHash | Nonce`. The `Hash` field itself is excluded to avoid circular dependency.
- **Simple proof-of-work**: Mining finds a hash with a constant leading-zero difficulty (`Difficulty = 4`). This is intentionally straightforward for demonstration and testing.
- **Pending transaction pool**: Transactions are validated before being appended to pending state. Balances include pending transactions when checking spend availability so double-spend attempts in the pending pool are blocked.
- **JSON persistence**: The chain is saved to and loaded from `blockchain.json` in the repo folder. This keeps state local and easy to inspect.
- **Single-node model**: The implementation focuses on local chain behavior, not distributed networking or consensus.

## Known Limitations

- No peer-to-peer networking or consensus algorithm is implemented.
- No cryptographic signatures or account authentication — transactions are only validated by sender balance.
- Balances are computed by simple subtraction/addition, not a full UTXO or account model.
- Mining difficulty is fixed and not dynamically adjusted.
- No transaction fees, mempool prioritization, or persistence safeguards beyond plain JSON.
- Mining is CPU-bound and sequential, which is fine for toy usage but not production scale.
- The `blockchain.json` file is overwritten on every save and does not support automatic backup or concurrency control.
