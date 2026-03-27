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
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		cl1Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, ls)
		cl2Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}, ls)
		sn.Start()

		// 2. Generate genesis
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		// 3. Start 2 separate EL nodes (each CL gets its own EL)
		el1 := elnode.Start(t, gen.ELGenesis)
		el2 := elnode.Start(t, gen.ELGenesis)

		// 4. Create QUIC transports
		cl1Opts, st1, _ := quicnet.NewSimnetTransport(cl1Conn)
		cl2Opts, st2, _ := quicnet.NewSimnetTransport(cl2Conn)

		// 5. Start 2 CL nodes
		bc1 := valnode.NewBufconnPair()
		bc2 := valnode.NewBufconnPair()
		cl1 := clnode.Start(t, clnode.Config{
			GenesisState: gen.CLState, BeaconConfig: gen.BeaconConfig,
			RPCClient: el1.Attach(), Libp2pOptions: cl1Opts,
			GRPCListener: bc1.GRPCListener, HTTPListener: bc1.HTTPListener,
		})
		cl2 := clnode.Start(t, clnode.Config{
			GenesisState: gen.CLState, BeaconConfig: gen.BeaconConfig,
			RPCClient: el2.Attach(), Libp2pOptions: cl2Opts,
			GRPCListener: bc2.GRPCListener, HTTPListener: bc2.HTTPListener,
		})

		// 6. Connect CL peers over simnet QUIC
		time.Sleep(1 * time.Second)
		p2p1 := cl1.P2PService()
		p2p2 := cl2.P2PService()
		if p2p1 != nil && p2p2 != nil && p2p2.Host() != nil {
			if err := p2p1.Connect(peer.AddrInfo{ID: p2p2.Host().ID(), Addrs: p2p2.Host().Addrs()}); err != nil {
				t.Fatalf("failed to connect peers: %v", err)
			}
			t.Log("CL peers connected via QUIC over simnet")
		}

		// 7. Start validators (32 each)
		v1 := valnode.Start(t, bc1, valnode.Config{NumValidators: 32, StartIndex: 0})
		v2 := valnode.Start(t, bc2, valnode.Config{NumValidators: 32, StartIndex: 32})

		// 8. Wait for finality (need ~4 epochs, 32 slots × 4s = 128s/epoch)
		t.Log("Waiting for finality...")
		time.Sleep(800 * time.Second)

		// 9. Assert finalized epoch
		epoch := cl1.FinalizedEpoch()
		t.Logf("Finalized epoch: %d", epoch)
		if epoch < 2 {
			t.Fatalf("expected finalized epoch >= 2, got %d", epoch)
		}

		// Shutdown
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
		time.Sleep(300 * time.Second)
	})
}
