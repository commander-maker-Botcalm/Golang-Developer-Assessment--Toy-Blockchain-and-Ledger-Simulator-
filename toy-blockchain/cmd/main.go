package main

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"toy-blockchain/blockchain"
	"toy-blockchain/node"
	"toy-blockchain/transaction"
)

// simply print help messages
func usage() {
	fmt.Println("Usage:")
	fmt.Println("  go run ./cmd init")
	fmt.Println("  go run ./cmd addtx <sender> <recipient> <amount> [--key <path>]")
	fmt.Println("  go run ./cmd generate-key <name>")
	fmt.Println("  go run ./cmd mine")
	fmt.Println("  go run ./cmd print")
	fmt.Println("  go run ./cmd balances")
	fmt.Println("  go run ./cmd balance <account>")
	fmt.Println("  go run ./cmd validate")
	fmt.Println("  go run ./cmd simulate-fork")
	fmt.Println("  go run ./cmd run-node --listen :8081 --peer http://localhost:8082 --file node1.json")
	fmt.Println()
	fmt.Println("Global flags:")
	fmt.Println("  --difficulty <n>   Mining difficulty (default: 4)")
	fmt.Println("  --blocksize <n>   Maximum transactions per block (default: 10)")
	fmt.Println("  --file <path>     Blockchain data file (default: blockchain.json)")
	fmt.Println("  --listen <addr>   Node listening address for HTTP service")
	fmt.Println("  --address <addr>  Alias for --listen")
	fmt.Println("  --peer <url>      Initial peer URL; may be repeated")
	fmt.Println("  --key <path>      PEM private key file used to sign addtx transactions")
}

func parseCLI(args []string) (string, []string, int, bool, int, bool, string, string, string, []string, error) {
	difficulty := -1
	blockSize := -1
	dataFile := blockchain.DefaultDataFile
	listenAddr := ""
	peers := []string{}
	difficultySet := false
	blockSizeSet := false
	keyFile := ""

	cmd := ""
	cmdArgs := []string{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--difficulty="):
			value := strings.TrimPrefix(arg, "--difficulty=")
			d, err := strconv.Atoi(value)
			if err != nil {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("invalid difficulty: %v", err)
			}
			difficulty = d
			difficultySet = true
		case strings.HasPrefix(arg, "--blocksize="):
			value := strings.TrimPrefix(arg, "--blocksize=")
			b, err := strconv.Atoi(value)
			if err != nil {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("invalid blocksize: %v", err)
			}
			blockSize = b
			blockSizeSet = true
		case strings.HasPrefix(arg, "--file="):
			dataFile = strings.TrimPrefix(arg, "--file=")
		case strings.HasPrefix(arg, "--listen="):
			listenAddr = strings.TrimPrefix(arg, "--listen=")
		case strings.HasPrefix(arg, "--address="):
			listenAddr = strings.TrimPrefix(arg, "--address=")
		case strings.HasPrefix(arg, "--peer="):
			peers = append(peers, strings.TrimPrefix(arg, "--peer="))
		case strings.HasPrefix(arg, "--key="):
			keyFile = strings.TrimPrefix(arg, "--key=")
		case arg == "--difficulty":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --difficulty")
			}
			d, err := strconv.Atoi(args[i+1])
			if err != nil {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("invalid difficulty: %v", err)
			}
			difficulty = d
			difficultySet = true
			i++
		case arg == "--blocksize":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --blocksize")
			}
			b, err := strconv.Atoi(args[i+1])
			if err != nil {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("invalid blocksize: %v", err)
			}
			blockSize = b
			blockSizeSet = true
			i++
		case arg == "--listen":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --listen")
			}
			listenAddr = args[i+1]
			i++
		case arg == "--address":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --address")
			}
			listenAddr = args[i+1]
			i++
		case arg == "--peer":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --peer")
			}
			peers = append(peers, args[i+1])
			i++
		case arg == "--file":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --file")
			}
			dataFile = args[i+1]
			i++
		case arg == "--key":
			if i+1 >= len(args) {
				return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("missing value for --key")
			}
			keyFile = args[i+1]
			i++
		case cmd == "" && isCommand(arg):
			cmd = arg
		case cmd == "" && !isCommand(arg):
			return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("unknown command: %s", arg)
		default:
			cmdArgs = append(cmdArgs, arg)
		}
	}

	if cmd == "" {
		return "", nil, 0, false, 0, false, "", "", "", nil, fmt.Errorf("command required")
	}

	return cmd, cmdArgs, difficulty, difficultySet, blockSize, blockSizeSet, dataFile, keyFile, listenAddr, peers, nil
}

func requiresPrivateKey(sender, keyFile string) bool {
	return sender != "SYSTEM" && keyFile == ""
}

func senderMatchesKeyFile(sender, keyFile string) bool {
	if keyFile == "" {
		return false
	}
	base := filepath.Base(keyFile)
	if ext := filepath.Ext(base); ext != "" {
		base = strings.TrimSuffix(base, ext)
	}
	return strings.EqualFold(sender, base)
}

func LoadPrivateKeyFromFile(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM from %s", path)
	}

	if block.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("unsupported PEM block type: %s", block.Type)
	}

	decoded, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	privateKey, ok := decoded.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("unsupported private key type: %T", decoded)
	}
	return privateKey, nil
}

func isCommand(arg string) bool {
	switch arg {
	case "init", "addtx", "generate-key", "mine", "print", "balances", "balance", "validate", "simulate-fork", "run-node":
		return true
	default:
		return false
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(1)
	}

	cmd, cmdArgs, difficulty, difficultySet, blockSize, blockSizeSet, dataFile, keyFile, listenAddr, peers, err := parseCLI(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to parse CLI flags: %v\n", err)
		os.Exit(1)
	}
	if cmd == "" {
		usage()
		os.Exit(1)
	}

	bc, err := blockchain.LoadFromFile(dataFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load blockchain: %v\n", err)
		os.Exit(1)
	}
	if difficultySet {
		bc.Difficulty = difficulty
	}
	if blockSizeSet {
		bc.BlockSize = blockSize
	}

	switch cmd {
	case "run-node":
		if listenAddr == "" {
			fmt.Fprintln(os.Stderr, "run-node requires --listen or --address")
			os.Exit(1)
		}
		nodeConfig := node.NodeConfig{
			ListenAddr: listenAddr,
			Peers:      peers,
			DataFile:   dataFile,
		}
		if difficultySet {
			nodeConfig.Difficulty = difficulty
		}
		if blockSizeSet {
			nodeConfig.BlockSize = blockSize
		}
		n := node.NewNode(nodeConfig)
		if err := n.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "failed to start node: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("node listening on %s\n", n.Config.ListenAddr)

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := n.Shutdown(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to shutdown node: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("node shutdown")
		return

	case "init":
		bc = blockchain.NewBlockchainWithConfig(difficulty, blockSize)
		if err := bc.SaveToFile(dataFile); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save blockchain: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("blockchain initialized")

	case "addtx":
		if len(cmdArgs) != 3 {
			fmt.Fprintln(os.Stderr, "addtx requires sender, recipient, and amount")
			usage()
			os.Exit(1)
		}
		if requiresPrivateKey(cmdArgs[0], keyFile) {
			fmt.Fprintln(os.Stderr, "addtx requires a private key file via --key <path> for non-SYSTEM transactions")
			os.Exit(1)
		}
		if cmdArgs[0] != "SYSTEM" && !senderMatchesKeyFile(cmdArgs[0], keyFile) {
			fmt.Fprintf(os.Stderr, "transaction sender %q must match the key file name %q\n", cmdArgs[0], keyFile)
			os.Exit(1)
		}
		amount, err := strconv.ParseFloat(cmdArgs[2], 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid amount: %v\n", err)
			os.Exit(1)
		}
		tx := transaction.Transaction{Sender: cmdArgs[0], Recipient: cmdArgs[1], Amount: amount}
		if cmdArgs[0] != "SYSTEM" {
			privateKey, err := LoadPrivateKeyFromFile(keyFile)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to load private key: %v\n", err)
				os.Exit(1)
			}
			publicKey := privateKey.Public().(ed25519.PublicKey)
			tx.PublicKey = transaction.PublicKeyToString(publicKey)
			signature, err := transaction.SignTransaction(&tx, privateKey)
			if err != nil {
				fmt.Fprintf(os.Stderr, "failed to sign transaction: %v\n", err)
				os.Exit(1)
			}
			tx.Signature = signature
		}

		if err := bc.AddTransaction(tx); err != nil {
			fmt.Fprintf(os.Stderr, "failed to add transaction: %v\n", err)
			os.Exit(1)
		}
		if err := bc.SaveToFile(dataFile); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save blockchain: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("transaction added")

	case "generate-key":
		if len(cmdArgs) != 1 {
			fmt.Fprintln(os.Stderr, "generate-key requires exactly one name")
			os.Exit(1)
		}
		name := strings.TrimSpace(cmdArgs[0])
		if name == "" {
			fmt.Fprintln(os.Stderr, "generate-key requires a non-empty name")
			os.Exit(1)
		}
		privateKey, err := transaction.GenerateKeyPair()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to generate key pair: %v\n", err)
			os.Exit(1)
		}
		encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to encode private key: %v\n", err)
			os.Exit(1)
		}
		path := fmt.Sprintf("%s.pem", name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}), 0o600); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write private key file: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("generated %s\n", path)

	case "mine":
		bc.MinePendingTransactions()
		if err := bc.SaveToFile(dataFile); err != nil {
			fmt.Fprintf(os.Stderr, "failed to save blockchain: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("pending transactions mined")

	case "print":
		bc.Print()

	case "balances":
		balances := bc.GetBalances()
		for account, balance := range balances {
			fmt.Printf("%s: %.8f\n", account, balance)
		}

	case "balance":
		if len(cmdArgs) != 1 {
			fmt.Fprintln(os.Stderr, "balance requires exactly one account name")
			usage()
			os.Exit(1)
		}
		account := cmdArgs[0]
		balances := bc.GetBalances()
		balance, ok := balances[account]
		if !ok {
			fmt.Fprintf(os.Stderr, "account not found: %s\n", account)
			os.Exit(1)
		}
		fmt.Printf("%s: %.8f\n", account, balance)

	case "validate":
		if err := bc.Validate(); err != nil {
			fmt.Printf("Validation failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Validation passed: blockchain is valid")

	case "simulate-fork":
		// Create two independent copies of the loaded chain.
		chainA := blockchain.CopyBlockchain(bc)
		chainB := blockchain.CopyBlockchain(bc)

		fmt.Println("Original chain:")
		bc.Print()
		fmt.Printf("Original chain length: %d\n\n", len(bc.Blocks))

		// Mine one different block on each chain starting from the same parent.
		fmt.Println("Mining divergent blocks on each chain...")
		txA := transaction.Transaction{Sender: "SYSTEM", Recipient: "ForkA", Amount: 1}
		txB := transaction.Transaction{Sender: "SYSTEM", Recipient: "ForkB", Amount: 2}

		_ = chainA.AddTransaction(txA)
		chainA.MinePendingTransactions()

		_ = chainB.AddTransaction(txB)
		chainB.MinePendingTransactions()

		// Extend chainA to make it longer (simulate longer fork)
		fmt.Println("Extending chain A to be longer...")
		for i := 0; i < 2; i++ {
			t := transaction.Transaction{Sender: "SYSTEM", Recipient: fmt.Sprintf("A_extra_%d", i), Amount: float64(i + 1)}
			_ = chainA.AddTransaction(t)
			chainA.MinePendingTransactions()
		}

		fmt.Println("\n--- Chain A ---")
		chainA.Print()
		fmt.Printf("Chain A length: %d\n", len(chainA.Blocks))
		if err := chainA.Validate(); err != nil {
			fmt.Printf("Chain A validation: failed: %v\n", err)
		} else {
			fmt.Println("Chain A validation: valid")
		}

		fmt.Println("\n--- Chain B ---")
		chainB.Print()
		fmt.Printf("Chain B length: %d\n", len(chainB.Blocks))
		if err := chainB.Validate(); err != nil {
			fmt.Printf("Chain B validation: failed: %v\n", err)
		} else {
			fmt.Println("Chain B validation: valid")
		}

		// Resolve the fork using the simple longest-valid-chain rule.
		winner := blockchain.ResolveFork(chainA, chainB)
		fmt.Println("\n--- Fork Resolution ---")
		if winner == chainA {
			fmt.Println("Winning chain: Chain A")
		} else {
			fmt.Println("Winning chain: Chain B")
		}

		fmt.Println("\n--- Adopted Chain (simulation) ---")
		winner.Print()

	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
		os.Exit(1)
	}
}
