// Package clnode wraps Prysm for in-process usage as a consensus layer node.
package clnode

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"testing"

	"net"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/das"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/db/filesystem"
	"github.com/spf13/afero"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/execution"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/node"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/libp2p/go-libp2p"
	"github.com/urfave/cli/v2"
)

// Config holds the configuration for creating a CL node.
type Config struct {
	// GenesisState is the CL genesis beacon state (native state.BeaconState).
	GenesisState state.BeaconState
	// BeaconConfig is the custom beacon chain config (fork epochs, timing, etc.)
	BeaconConfig *params.BeaconChainConfig
	// RPCClient is the in-process RPC client connected to the paired EL node.
	RPCClient *rpc.Client
	// Libp2pOptions are custom libp2p options (e.g. simnet QUIC transport).
	Libp2pOptions []libp2p.Option
	// DataDir is the directory for node data. Uses t.TempDir() if empty.
	DataDir string
	// MaxPeers sets the maximum number of P2P peers.
	MaxPeers uint
	// QueueSize sets the pubsub queue size.
	QueueSize uint
	// GRPCListener is an optional custom listener for the gRPC server (e.g. bufconn).
	GRPCListener net.Listener
	// HTTPListener is an optional custom listener for the HTTP REST server (e.g. bufconn).
	HTTPListener net.Listener
}

// Node wraps a Prysm beacon node.
type Node struct {
	Beacon *node.BeaconNode
	cancel context.CancelFunc
}

// genesisProvider implements genesis.Provider using an in-memory beacon state.
type genesisProvider struct {
	st state.BeaconState
}

func (p *genesisProvider) Genesis(_ context.Context) (state.BeaconState, error) {
	return p.st, nil
}

// Start creates and returns a Prysm beacon node configured for in-process testing.
func Start(t *testing.T, cfg Config) *Node {
	t.Helper()

	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}
	if cfg.MaxPeers == 0 {
		cfg.MaxPeers = 10
	}
	if cfg.QueueSize == 0 {
		cfg.QueueSize = 600
	}

	provider := &genesisProvider{st: cfg.GenesisState}

	// Build synthetic CLI context with minimal required flags.
	app := &cli.App{}
	set := flag.NewFlagSet("test-beacon", 0)
	set.String("datadir", cfg.DataDir, "data directory")
	set.String("deposit-contract", params.BeaconConfig().DepositContractAddress, "deposit contract")
	set.String("suggested-fee-recipient", "0x0000000000000000000000000000000000000000", "fee recipient")
	set.Bool("no-discovery", true, "disable discovery")
	set.String("p2p-encoding", "ssz-snappy", "p2p encoding")
	set.Bool("disable-monitoring", true, "disable monitoring")

	// Create parent context
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	parent := &cli.Context{Context: ctx}
	cliCtx := cli.NewContext(app, set, parent)

	// Build P2P config
	p2pCfg := &p2p.Config{
		NoDiscovery:         true,
		DataDir:             cfg.DataDir,
		DiscoveryDir:        filepath.Join(cfg.DataDir, "discovery"),
		MaxPeers:            cfg.MaxPeers,
		QueueSize:           cfg.QueueSize,
		CustomLibp2pOptions: cfg.Libp2pOptions,
	}

	// Build node options
	var configOpts []node.Option
	if cfg.BeaconConfig != nil {
		bcfg := cfg.BeaconConfig
		configOpts = append(configOpts, node.WithConfigOptions(func(c *params.BeaconChainConfig) {
			*c = *bcfg
		}))
	}

	// Use the cancellable context for data column storage so its goroutines
	// exit when the node is closed.
	dcs := filesystem.NewEphemeralDataColumnStorageWithCtx(t, ctx, afero.NewMemMapFs())
	dcs.WarmCache()

	opts := []node.Option{
		node.WithBlobStorage(filesystem.NewEphemeralBlobStorage(t)),
		node.WithDataColumnStorage(dcs),
		node.WithP2PConfig(p2pCfg),
		node.WithSkipSignalHandler(),
	}
	if cfg.GRPCListener != nil {
		opts = append(opts, node.WithGRPCListener(cfg.GRPCListener))
	}
	if cfg.HTTPListener != nil {
		opts = append(opts, node.WithHTTPListener(cfg.HTTPListener))
	}
	opts = append(opts,
		// Inject the RPC client for Engine API
		node.WithExecutionChainOptions([]execution.Option{
			execution.WithRPCClient(cfg.RPCClient),
		}),
		// Add genesis provider and sync needs waiter
		func(bn *node.BeaconNode) error {
			bn.GenesisProviders = append(bn.GenesisProviders, provider)
			bn.SyncNeedsWaiter = func() (das.SyncNeeds, error) {
				return das.SyncNeeds{}, nil
			}
			return nil
		},
	)

	opts = append(configOpts, opts...)
	beacon, err := node.New(cliCtx, cancel, nil, opts...)
	if err != nil {
		t.Fatalf("failed to create beacon node: %v", err)
	}
	// Start the beacon node
	go beacon.Start()

	return &Node{
		Beacon: beacon,
		cancel: cancel,
	}
}

// Close stops the beacon node and cancels its context.
func (n *Node) Close() {
	n.cancel()
	n.Beacon.Close()
}

// PeerInfo returns the peer information string for connecting other nodes to this one.
func (n *Node) PeerInfo() string {
	// TODO: implement once P2P is wired up
	return fmt.Sprintf("peer-info-placeholder")
}
