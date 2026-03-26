// Package clnode wraps Prysm for in-process usage as a consensus layer node.
package clnode

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/db/filesystem"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/execution"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/node"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/p2p"
	"github.com/OffchainLabs/prysm/v7/beacon-chain/state"
	statenative "github.com/OffchainLabs/prysm/v7/beacon-chain/state/state-native"
	"github.com/OffchainLabs/prysm/v7/config/params"
	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/libp2p/go-libp2p"
	"github.com/urfave/cli/v2"
)

// Config holds the configuration for creating a CL node.
type Config struct {
	// GenesisState is the CL genesis beacon state.
	GenesisState *ethpb.BeaconState
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
}

// Node wraps a Prysm beacon node.
type Node struct {
	Beacon *node.BeaconNode
}

// genesisProvider implements genesis.Provider using an in-memory beacon state.
type genesisProvider struct {
	state state.BeaconState
}

func (p *genesisProvider) Genesis(_ context.Context) (state.BeaconState, error) {
	return p.state, nil
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

	// Convert the proto beacon state to a native state for the genesis provider.
	nativeState, err := statenative.InitializeFromProtoUnsafePhase0(cfg.GenesisState)
	if err != nil {
		t.Fatalf("failed to initialize native state from proto: %v", err)
	}

	provider := &genesisProvider{state: nativeState}

	// Write genesis state to SSZ file for Prysm's initialization flow.
	genesisSSZ, err := nativeState.MarshalSSZ()
	if err != nil {
		t.Fatalf("failed to marshal genesis state: %v", err)
	}
	genesisPath := filepath.Join(cfg.DataDir, "genesis.ssz")
	if err := os.WriteFile(genesisPath, genesisSSZ, 0644); err != nil {
		t.Fatalf("failed to write genesis SSZ: %v", err)
	}

	// Build synthetic CLI context with minimal required flags.
	app := &cli.App{}
	set := flag.NewFlagSet("test-beacon", 0)
	set.Bool("test-skip-pow", true, "skip pow dial")
	set.String("datadir", cfg.DataDir, "data directory")
	set.String("genesis-state", genesisPath, "genesis state path")
	set.String("deposit-contract", params.BeaconConfig().DepositContractAddress, "deposit contract")
	set.String("suggested-fee-recipient", "0x0000000000000000000000000000000000000000", "fee recipient")
	set.Bool("no-discovery", true, "disable discovery")
	set.String("p2p-encoding", "ssz-snappy", "p2p encoding")
	set.Bool("disable-monitoring", true, "disable monitoring")
	// Set interop validator flags
	set.Uint64("interop-num-validators", 0, "number of interop validators")
	set.Uint64("interop-start-index", 0, "interop start index")

	// Create parent context
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	parent := &cli.Context{Context: ctx}
	cliCtx := cli.NewContext(app, set, parent)

	// Build P2P config
	p2pCfg := &p2p.Config{
		NoDiscovery:       true,
		DataDir:           cfg.DataDir,
		DiscoveryDir:      filepath.Join(cfg.DataDir, "discovery"),
		MaxPeers:          cfg.MaxPeers,
		QueueSize:         cfg.QueueSize,
		CustomLibp2pOptions: cfg.Libp2pOptions,
	}

	// Build node options
	opts := []node.Option{
		node.WithBlobStorage(filesystem.NewEphemeralBlobStorage(t)),
		node.WithDataColumnStorage(filesystem.NewEphemeralDataColumnStorage(t)),
		node.WithP2PConfig(p2pCfg),
		// Inject the RPC client for Engine API
		node.WithExecutionChainOptions([]execution.Option{
			execution.WithRPCClient(cfg.RPCClient),
		}),
		// Add genesis provider
		func(bn *node.BeaconNode) error {
			bn.GenesisProviders = append(bn.GenesisProviders, provider)
			return nil
		},
	}

	beacon, err := node.New(cliCtx, cancel, nil, opts...)
	if err != nil {
		t.Fatalf("failed to create beacon node: %v", err)
	}
	t.Cleanup(func() { beacon.Close() })

	// Start the beacon node
	go beacon.Start()

	return &Node{
		Beacon: beacon,
	}
}

// PeerInfo returns the peer information string for connecting other nodes to this one.
func (n *Node) PeerInfo() string {
	// TODO: implement once P2P is wired up
	return fmt.Sprintf("peer-info-placeholder")
}
