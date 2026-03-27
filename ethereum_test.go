package main

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/go-test-ethereum/pkg/valnode"
	"github.com/marcopolo/simnet"
)

func TestEthereum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// 1. Setup simnet
		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
		}
		linkSettings := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}

		cl1Addr := &net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}
		cl2Addr := &net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}

		cl1Conn := sn.NewEndpoint(cl1Addr, linkSettings)
		cl2Conn := sn.NewEndpoint(cl2Addr, linkSettings)
		sn.Start()

		// 2. Generate genesis
		t.Log("Generating genesis...")
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})
		t.Log("Genesis generated successfully")

		// 3. Start EL nodes
		t.Log("Starting EL nodes...")
		el1 := elnode.Start(t, gen.ELGenesis)
		el2 := elnode.Start(t, gen.ELGenesis)
		t.Log("EL nodes started")

		// 4. Create QUIC-over-simnet transports
		cl1Opts, st1, err := quicnet.NewSimnetTransport(cl1Conn)
		if err != nil {
			t.Fatalf("failed to create simnet transport for CL1: %v", err)
		}
		cl2Opts, st2, err := quicnet.NewSimnetTransport(cl2Conn)
		if err != nil {
			t.Fatalf("failed to create simnet transport for CL2: %v", err)
		}

		// 5. Create bufconn pairs for validator→beacon
		bc1 := valnode.NewBufconnPair()
		bc2 := valnode.NewBufconnPair()

		// 6. Start CL nodes
		t.Log("Starting CL nodes...")
		cl1 := clnode.Start(t, clnode.Config{
			GenesisState:  gen.CLState,
			BeaconConfig:  gen.BeaconConfig,
			RPCClient:     el1.Attach(),
			Libp2pOptions: cl1Opts,
			GRPCListener:  bc1.GRPCListener,
			HTTPListener:  bc1.HTTPListener,
		})
		cl2 := clnode.Start(t, clnode.Config{
			GenesisState:  gen.CLState,
			BeaconConfig:  gen.BeaconConfig,
			RPCClient:     el2.Attach(),
			Libp2pOptions: cl2Opts,
			GRPCListener:  bc2.GRPCListener,
			HTTPListener:  bc2.HTTPListener,
		})
		t.Log("CL nodes started")

		// 6b. Connect CL peers over simnet QUIC
		// Wait a moment for the P2P hosts to initialize
		time.Sleep(1 * time.Second)
		p2p1 := cl1.P2PService()
		p2p2 := cl2.P2PService()
		if p2p1 != nil && p2p2 != nil && p2p2.Host() != nil {
			addrs2 := p2p2.Host().Addrs()
			id2 := p2p2.Host().ID()
			t.Logf("Connecting CL1 to CL2 (id=%s, addrs=%v)", id2, addrs2)
			err = p2p1.Connect(peer.AddrInfo{ID: id2, Addrs: addrs2})
			if err != nil {
				t.Logf("peer connection failed: %v", err)
			} else {
				t.Log("CL peers connected")
			}
		} else {
			t.Log("P2P services not ready, skipping peer connection")
		}

		// 7. Start validators
		t.Log("Starting validators...")
		v1 := valnode.Start(t, bc1, valnode.Config{
			NumValidators: 32,
			StartIndex:    0,
		})
		v2 := valnode.Start(t, bc2, valnode.Config{
			NumValidators: 32,
			StartIndex:    32,
		})
		t.Log("Validators started")

		// 8. Wait for 2 epochs (synctest advances time when goroutines block)
		// Need ~4 epochs for finality: justify epoch 0 at boundary of epoch 1,
		// finalize epoch 0 at boundary of epoch 2. 32 slots × 4s = 128s/epoch.
		// 4 epochs = 512s + genesis delay.
		t.Log("Waiting for 4 epochs...")
		time.Sleep(550 * time.Second)

		// TODO: Assert finalized epoch agreement
		t.Log("Ethereum network ran for 2 epochs")

		// Shut down everything so all goroutines exit cleanly.
		// synctest requires all bubble goroutines to exit.
		v1.Close()
		v2.Close()
		cl1.Close()
		cl2.Close()
		st1.ConnManager.Close()
		st2.ConnManager.Close()
		cl1Conn.Close()
		cl2Conn.Close()
		el1.Stack.Close()
		el2.Stack.Close()
		sn.Close()

		// Allow time for all goroutines to process shutdown
		time.Sleep(300 * time.Second)
	})
}
