// Package elnode wraps go-ethereum for in-process usage as an execution layer node.
package elnode

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/rpc"
)

// Node wraps a go-ethereum node running as an execution layer client.
type Node struct {
	Stack   *node.Node
	Eth     *eth.Ethereum
}

// Start creates and starts an EL node with P2P disabled.
// The returned Node can be used to get an in-process RPC client via Attach().
func Start(t *testing.T, genesis *core.Genesis) *Node {
	t.Helper()

	stack, err := node.New(&node.Config{
		DataDir: t.TempDir(),
		P2P: p2p.Config{
			ListenAddr:  "",
			MaxPeers:    0,
			NoDiscovery: true,
			NoDial:      true,
		},
		// No HTTP/WS/Auth servers needed - we use in-process RPC
	})
	if err != nil {
		t.Fatalf("failed to create geth node: %v", err)
	}

	ethCfg := &ethconfig.Config{
		Genesis:        genesis,
		SyncMode:       ethconfig.FullSync,
		TrieTimeout:    60 * time.Minute,
		TrieCleanCache: 256,
		TrieDirtyCache: 256,
		SnapshotCache:  256,
		Miner:          miner.DefaultConfig,
	}

	ethService, err := eth.New(stack, ethCfg)
	if err != nil {
		t.Fatalf("failed to create eth service: %v", err)
	}

	if err := stack.Start(); err != nil {
		t.Fatalf("failed to start geth node: %v", err)
	}
	t.Cleanup(func() { stack.Close() })

	ethService.SetSynced()

	return &Node{
		Stack: stack,
		Eth:   ethService,
	}
}

// Attach returns an in-process RPC client connected to this node.
// This is used by the consensus layer to communicate via Engine API.
func (n *Node) Attach() *rpc.Client {
	return n.Stack.Attach()
}
