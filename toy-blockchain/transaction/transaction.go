package transaction

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Transaction represents a transfer of value between two parties.
// It carries the sender's public key and a signature for authorization.
type Transaction struct {
	Sender    string  `json:"sender"`
	Recipient string  `json:"recipient"`
	Amount    float64 `json:"amount"`
	PublicKey string  `json:"publicKey"`
	Signature string  `json:"signature"`
}

// String returns a human-readable representation of a Transaction.
func (tx Transaction) String() string {
	return fmt.Sprintf("%s → %s : %.8f", tx.Sender, tx.Recipient, tx.Amount)
}

// TransactionID creates a deterministic ID for a transaction from its canonical fields.
func TransactionID(tx Transaction) string {
	payload := fmt.Sprintf("%s|%s|%.8f|%s|%s", tx.Sender, tx.Recipient, tx.Amount, tx.PublicKey, tx.Signature)
	sum := sha256.Sum256([]byte(payload))
	return hex.EncodeToString(sum[:])
}

// GenerateKeyPair creates a new Ed25519 private/public key pair for transaction signing.
func GenerateKeyPair() (ed25519.PrivateKey, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	_ = publicKey
	return privateKey, nil
}

// PublicKeyToString serializes an Ed25519 public key to a hex string.
func PublicKeyToString(pub ed25519.PublicKey) string {
	return hex.EncodeToString(pub)
}

// StringToPublicKey deserializes a hex string back to an Ed25519 public key.
func StringToPublicKey(pubKeyStr string) (ed25519.PublicKey, error) {
	publicKeyBytes, err := hex.DecodeString(pubKeyStr)
	if err != nil {
		return nil, err
	}
	if len(publicKeyBytes) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid public key length: expected %d, got %d", ed25519.PublicKeySize, len(publicKeyBytes))
	}
	return ed25519.PublicKey(publicKeyBytes), nil
}

// SignTransaction creates an Ed25519 signature for a transaction using the private key.
// Returns the signature as a hex string.
func SignTransaction(tx *Transaction, privateKey ed25519.PrivateKey) (string, error) {
	messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%.8f", tx.Sender, tx.Recipient, tx.Amount)))
	signature := ed25519.Sign(privateKey, messageHash[:])
	return hex.EncodeToString(signature), nil
}

// VerifyTransaction checks if the transaction's signature is valid using its public key.
func VerifyTransaction(tx *Transaction) (bool, error) {
	if tx.PublicKey == "" || tx.Signature == "" {
		return false, fmt.Errorf("transaction missing public key or signature")
	}

	publicKey, err := StringToPublicKey(tx.PublicKey)
	if err != nil {
		return false, err
	}

	signatureBytes, err := hex.DecodeString(tx.Signature)
	if err != nil {
		return false, err
	}
	if len(signatureBytes) != ed25519.SignatureSize {
		return false, fmt.Errorf("invalid signature length: expected %d, got %d", ed25519.SignatureSize, len(signatureBytes))
	}

	messageHash := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%.8f", tx.Sender, tx.Recipient, tx.Amount)))
	return ed25519.Verify(publicKey, messageHash[:], signatureBytes), nil
}
