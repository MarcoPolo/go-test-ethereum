// Package elnode wraps go-ethereum for in-process usage as an execution layer node.
package elnode

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/eth/catalyst"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rpc"
)

// Node wraps a go-ethereum node running as an execution layer client.
type Node struct {
	Stack *node.Node
	Eth   *eth.Ethereum
}

// Config holds optional configuration for the EL node.
type Config struct {
	// ListenFunc overrides the P2P listener creation. If nil, P2P is disabled.
	ListenFunc func(network, addr string) (net.Listener, error)
	// Dialer overrides the P2P dialer. If nil, P2P dialing is disabled.
	Dialer p2p.NodeDialer
	// ListenAddr is the P2P listen address. Required if ListenFunc is set.
	ListenAddr string
	// StaticPeers are enode URLs to connect to.
	StaticPeers []*enode.Node
}

// Start creates and starts an EL node.
func Start(t *testing.T, genesis *core.Genesis, cfgs ...Config) *Node {
	t.Helper()
	var cfg Config
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}

	p2pCfg := p2p.Config{
		NoDiscovery: true,
		MaxPeers:    10,
	}

	if cfg.ListenFunc != nil {
		p2pCfg.ListenAddr = cfg.ListenAddr
	} else {
		// No P2P
		p2pCfg.ListenAddr = ""
		p2pCfg.MaxPeers = 0
		p2pCfg.NoDial = true
	}

	stack, err := node.New(&node.Config{
		DataDir: "", // in-memory database
		P2P:     p2pCfg,
	})
	if err != nil {
		t.Fatalf("failed to create geth node: %v", err)
	}

	// Inject custom listen function and dialer for QUIC-over-simnet P2P
	if cfg.ListenFunc != nil {
		stack.Server().ListenFunc = cfg.ListenFunc
	}
	if cfg.Dialer != nil {
		stack.Server().Dialer = cfg.Dialer
	}
	if len(cfg.StaticPeers) > 0 {
		for _, peer := range cfg.StaticPeers {
			stack.Server().AddTrustedPeer(peer)
			stack.Server().AddPeer(peer)
		}
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

	// Register the Engine API as non-authenticated for in-process RPC
	stack.RegisterAPIs([]rpc.API{
		{
			Namespace: "engine",
			Service:   catalyst.NewConsensusAPI(ethService),
		},
	})

	if err := stack.Start(); err != nil {
		t.Fatalf("failed to start geth node: %v", err)
	}

	ethService.SetSynced()

	return &Node{Stack: stack, Eth: ethService}
}

// Attach returns an in-process RPC client connected to this node.
func (n *Node) Attach() *rpc.Client {
	return n.Stack.Attach()
}

// Enode returns the enode URL for this node.
func (n *Node) Enode() *enode.Node {
	return n.Stack.Server().Self()
}

// QUICDialer implements p2p.NodeDialer for dialing over QUIC streams.
type QUICDialer struct {
	DialFunc func(ctx context.Context, addr net.Addr) (net.Conn, error)
}

func (d *QUICDialer) Dial(ctx context.Context, dest *enode.Node) (net.Conn, error) {
	ep, _ := dest.TCPEndpoint()
	addr := net.UDPAddrFromAddrPort(ep)
	return d.DialFunc(ctx, addr)
}
