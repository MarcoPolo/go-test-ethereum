package main

import (
	"net"
	"testing"
	"time"

	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/simnet"
)

func TestEthereum(t *testing.T) {
	// Start without synctest first to verify basic wiring works.
	// Will add synctest.Test wrapper once initialization is confirmed working.

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

	// 2. Generate genesis (EL + CL, minimal config, 64 validators)
	t.Log("Generating genesis...")
	gen := genesis.Generate(t, genesis.Config{
		NumValidators: 64,
		GenesisTime:   time.Now().Add(10 * time.Second), // genesis 10s in the future
	})
	t.Log("Genesis generated successfully")

	// 3. Start EL nodes (geth as library, no P2P)
	t.Log("Starting EL nodes...")
	el1 := elnode.Start(t, gen.ELGenesis)
	t.Log("EL1 started")
	el2 := elnode.Start(t, gen.ELGenesis)
	t.Log("EL2 started")

	// 4. Create QUIC-over-simnet transports for CL P2P
	cl1Opts, _, err := quicnet.NewSimnetTransport(cl1Conn)
	if err != nil {
		t.Fatalf("failed to create simnet transport for CL1: %v", err)
	}
	cl2Opts, _, err := quicnet.NewSimnetTransport(cl2Conn)
	if err != nil {
		t.Fatalf("failed to create simnet transport for CL2: %v", err)
	}

	// 5. Start CL nodes
	t.Log("Starting CL node 1...")
	_ = clnode.Start(t, clnode.Config{
		GenesisState:  gen.CLState,
		RPCClient:     el1.Attach(),
		Libp2pOptions: cl1Opts,
	})
	t.Log("CL1 started")

	t.Log("Starting CL node 2...")
	_ = clnode.Start(t, clnode.Config{
		GenesisState:  gen.CLState,
		RPCClient:     el2.Attach(),
		Libp2pOptions: cl2Opts,
	})
	t.Log("CL2 started")

	// 6. Wait for 2 epochs
	// With minimal config: 8 slots/epoch, 6s/slot = 48s/epoch
	// 2 epochs = 96s
	t.Log("Waiting for 2 epochs...")
	time.Sleep(100 * time.Second)
	t.Log("Ethereum network ran for 2 epochs")
}
