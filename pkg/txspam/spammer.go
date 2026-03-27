// Package txspam generates transactions (including blob txs) and sends them to an EL node.
package txspam

import (
	"context"
	"crypto/ecdsa"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/holiman/uint256"
)

// The prefunded test account from Prysm's interop genesis.
// Address: 0x878705ba3f8bc32fcf7f4caa1a35e72af65cf766
// This account is funded with 100M ETH in the genesis allocation.
// We derive a child key from a known seed for sending transactions.
var testKey *ecdsa.PrivateKey

func init() {
	// Use a well-known test key. This key's address must be funded in genesis.
	// Key from go-ethereum's test fixtures.
	var err error
	testKey, err = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	if err != nil {
		panic(err)
	}
}

// TestAddress returns the address of the test key.
func TestAddress() common.Address {
	return crypto.PubkeyToAddress(testKey.PublicKey)
}

// Start begins sending transactions (including blob txs) to the EL node.
// It runs in a goroutine and stops when ctx is cancelled.
func Start(ctx context.Context, t *testing.T, rpcClient *rpc.Client, chainID *big.Int, interval time.Duration) {
	client := ethclient.NewClient(rpcClient)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		sender := TestAddress()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				nonce, err := client.PendingNonceAt(ctx, sender)
				if err != nil {
					continue
				}
				// Every 5th tx is a blob tx
				if nonce%5 == 4 {
					err = sendBlobTx(ctx, client, chainID, nonce)
				} else {
					err = sendTransferTx(ctx, client, chainID, nonce)
				}
				if err != nil {
					t.Logf("tx send error (nonce=%d): %v", nonce, err)
				}
			}
		}
	}()
}

func sendTransferTx(ctx context.Context, client *ethclient.Client, chainID *big.Int, nonce uint64) error {
	to := common.HexToAddress("0x1111111111111111111111111111111111111111")
	tx := types.NewTx(&types.DynamicFeeTx{
		ChainID:   chainID,
		Nonce:     nonce,
		GasTipCap: big.NewInt(1_000_000_000),  // 1 gwei
		GasFeeCap: big.NewInt(10_000_000_000), // 10 gwei
		Gas:       21000,
		To:        &to,
		Value:     big.NewInt(1000), // tiny amount
	})

	signer := types.NewCancunSigner(chainID)
	signed, err := types.SignTx(tx, signer, testKey)
	if err != nil {
		return err
	}
	return client.SendTransaction(ctx, signed)
}

func sendBlobTx(ctx context.Context, client *ethclient.Client, chainID *big.Int, nonce uint64) error {
	to := common.HexToAddress("0x2222222222222222222222222222222222222222")

	// Create a blob with some test data
	var blob kzg4844.Blob
	copy(blob[:], []byte("hello blob"))

	commit, err := kzg4844.BlobToCommitment(&blob)
	if err != nil {
		return err
	}

	// Fulu/PeerDAS uses cell proofs (128 per blob) instead of single blob proof
	cellProofs, err := kzg4844.ComputeCellProofs(&blob)
	if err != nil {
		return err
	}

	sidecar := types.NewBlobTxSidecar(
		types.BlobSidecarVersion1, // cell proofs for Osaka/Fulu
		[]kzg4844.Blob{blob},
		[]kzg4844.Commitment{commit},
		cellProofs,
	)

	tx := types.NewTx(&types.BlobTx{
		ChainID:    uint256.MustFromBig(chainID),
		Nonce:      nonce,
		GasTipCap:  uint256.NewInt(1_000_000_000),
		GasFeeCap:  uint256.NewInt(10_000_000_000),
		Gas:        21000,
		To:         to,
		Value:      uint256.NewInt(1000),
		BlobFeeCap: uint256.NewInt(1_000_000_000), // 1 gwei per blob gas
		BlobHashes: sidecar.BlobHashes(),
		Sidecar:    sidecar,
	})

	signer := types.NewCancunSigner(chainID)
	signed, err := types.SignTx(tx, signer, testKey)
	if err != nil {
		return err
	}
	return client.SendTransaction(ctx, signed)
}
