package main

import (
	"net"
	"testing"
	"time"

	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/go-test-ethereum/pkg/valnode"
	"github.com/marcopolo/simnet"
)

func TestEthereum(t *testing.T) {
	// 1. Setup simnet
	sn := &simnet.Simnet{}
	linkSettings := simnet.NodeBiDiLinkSettings{
		Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
	}

	cl1Addr := &net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}
	cl2Addr := &net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}

	cl1Conn := sn.NewEndpoint(cl1Addr, linkSettings)
	cl2Conn := sn.NewEndpoint(cl2Addr, linkSettings)
	sn.Start()
	t.Cleanup(func() { sn.Close() })

	// 2. Generate genesis (EL + CL, 64 validators, Fulu fork)
	t.Log("Generating genesis...")
	gen := genesis.Generate(t, genesis.Config{
		NumValidators: 64,
		GenesisTime:   time.Now().Add(10 * time.Second),
	})
	t.Log("Genesis generated successfully")

	// 3. Start EL nodes (geth as library, no P2P)
	t.Log("Starting EL nodes...")
	el1 := elnode.Start(t, gen.ELGenesis)
	el2 := elnode.Start(t, gen.ELGenesis)
	t.Log("EL nodes started")

	// 4. Create QUIC-over-simnet transports for CL P2P
	cl1Opts, _, err := quicnet.NewSimnetTransport(cl1Conn)
	if err != nil {
		t.Fatalf("failed to create simnet transport for CL1: %v", err)
	}
	cl2Opts, _, err := quicnet.NewSimnetTransport(cl2Conn)
	if err != nil {
		t.Fatalf("failed to create simnet transport for CL2: %v", err)
	}

	// 5. Create bufconn pairs for validator→beacon gRPC (no TCP)
	bc1 := valnode.NewBufconnPair()
	bc2 := valnode.NewBufconnPair()

	// 6. Start CL nodes with bufconn gRPC+HTTP listeners
	t.Log("Starting CL nodes...")
	_ = clnode.Start(t, clnode.Config{
		GenesisState:  gen.CLState,
		BeaconConfig:  gen.BeaconConfig,
		RPCClient:     el1.Attach(),
		Libp2pOptions: cl1Opts,
		GRPCListener:  bc1.GRPCListener,
		HTTPListener:  bc1.HTTPListener,
	})
	_ = clnode.Start(t, clnode.Config{
		GenesisState:  gen.CLState,
		BeaconConfig:  gen.BeaconConfig,
		RPCClient:     el2.Attach(),
		Libp2pOptions: cl2Opts,
		GRPCListener:  bc2.GRPCListener,
		HTTPListener:  bc2.HTTPListener,
	})
	t.Log("CL nodes started")

	// 7. Start validators (32 each, interop keys, connected via bufconn)
	t.Log("Starting validators...")
	valnode.Start(t, bc1, valnode.Config{
		NumValidators: 32,
		StartIndex:    0,
	})
	valnode.Start(t, bc2, valnode.Config{
		NumValidators: 32,
		StartIndex:    32,
	})
	t.Log("Validators started")

	// 8. Wait for 2 epochs
	// Config: 32 slots/epoch, 4s/slot = 128s/epoch, 2 epochs = 256s + genesis delay
	t.Log("Waiting for 2 epochs...")
	time.Sleep(270 * time.Second)

	// TODO: Assert finalized epoch agreement between nodes
	t.Log("Ethereum network ran for 2 epochs")
}
