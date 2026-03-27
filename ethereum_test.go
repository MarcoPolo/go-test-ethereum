package main

import (
	"context"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/core"
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
		// 1. Setup simnet with 4 endpoints:
		//    - 2 for CL P2P (libp2p QUIC)
		//    - 2 for EL P2P (TCP-over-QUIC streams)
		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
		}
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		cl1Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, ls)
		cl2Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}, ls)
		el1Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 30303}, ls)
		el2Conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 30303}, ls)
		sn.Start()

		// 2. Generate genesis
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		// 3. Create EL P2P listeners (TCP-over-QUIC-over-simnet)
		el1Lis, err := quicnet.NewQUICStreamListener(el1Conn)
		if err != nil {
			t.Fatalf("failed to create EL1 listener: %v", err)
		}
		el2Lis, err := quicnet.NewQUICStreamListener(el2Conn)
		if err != nil {
			t.Fatalf("failed to create EL2 listener: %v", err)
		}

		// 4. Start 2 EL nodes with QUIC P2P
		el2Addr := el2Conn.LocalAddr() // the simnet address of EL2
		el1Addr := el1Conn.LocalAddr()
		el1 := elnode.Start(t, gen.ELGenesis, elnode.Config{
			ListenFunc: func(_, _ string) (net.Listener, error) { return el1Lis, nil },
			Dialer: &elnode.QUICDialer{DialFunc: func(ctx context.Context, _ net.Addr) (net.Conn, error) {
				// Always dial EL2's simnet address
				return quicnet.DialQUICStream(ctx, el1Conn, el2Addr)
			}},
			ListenAddr: el1Addr.String(),
		})
		el2 := elnode.Start(t, gen.ELGenesis, elnode.Config{
			ListenFunc: func(_, _ string) (net.Listener, error) { return el2Lis, nil },
			Dialer: &elnode.QUICDialer{DialFunc: func(ctx context.Context, _ net.Addr) (net.Conn, error) {
				// Always dial EL1's simnet address
				return quicnet.DialQUICStream(ctx, el2Conn, el1Addr)
			}},
			ListenAddr: el2Addr.String(),
		})

		// Connect EL peers after a brief delay for server initialization
		time.Sleep(100 * time.Millisecond)
		t.Logf("EL1 enode: %v", el1.Enode().URLv4())
		t.Logf("EL2 enode: %v", el2.Enode().URLv4())
		t.Logf("EL1 server peer count: %d", el1.Stack.Server().PeerCount())
		el1.Stack.Server().AddPeer(el2.Enode())
		el2.Stack.Server().AddPeer(el1.Enode())
		time.Sleep(2 * time.Second)
		t.Logf("EL1 peer count after add: %d", el1.Stack.Server().PeerCount())
		t.Logf("EL2 peer count after add: %d", el2.Stack.Server().PeerCount())
		t.Log("EL nodes started and peered")

		// 5. Create CL QUIC transports
		cl1Opts, st1, _ := quicnet.NewSimnetTransport(cl1Conn)
		cl2Opts, st2, _ := quicnet.NewSimnetTransport(cl2Conn)

		// 6. Start 2 CL nodes
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

		// 7. Connect CL peers
		time.Sleep(1 * time.Second)
		p2p1 := cl1.P2PService()
		p2p2 := cl2.P2PService()
		if err := p2p1.Connect(peer.AddrInfo{ID: p2p2.Host().ID(), Addrs: p2p2.Host().Addrs()}); err != nil {
			t.Fatalf("failed to connect CL peers: %v", err)
		}
		t.Log("CL peers connected via QUIC over simnet")

		// 8. Start validators (all 64 on CL1 for now to avoid conflicting proposals)
		v1 := valnode.Start(t, bc1, valnode.Config{NumValidators: 64, StartIndex: 0})
		_ = bc2 // CL2 runs without validators — just syncs via P2P

		// 9. Wait for finality
		t.Log("Waiting for finality...")
		time.Sleep(800 * time.Second)

		// 10. Assert finalized epoch on both nodes
		e1 := cl1.FinalizedEpoch()
		e2 := cl2.FinalizedEpoch()
		t.Logf("Node 1 finalized epoch: %d, Node 2 finalized epoch: %d", e1, e2)
		if e1 < 2 || e2 < 2 {
			t.Fatalf("expected both nodes finalized epoch >= 2, got %d and %d", e1, e2)
		}
		if e1 != e2 {
			t.Fatalf("nodes disagree on finalized epoch: %d vs %d", e1, e2)
		}
		t.Logf("SUCCESS: Both nodes agree on finalized epoch %d", e1)

		// Shutdown — order matters for clean goroutine exit
		v1.Close()
		cl1.Close()
		cl2.Close()
		st1.ConnManager.Close()
		st2.ConnManager.Close()
		el1Lis.Close()
		el2Lis.Close()
		cl1Conn.Close()
		cl2Conn.Close()
		el1Conn.Close()
		el2Conn.Close()
		el1.Stack.Close()
		el2.Stack.Close()
		core.SenderCacher().Close()
		sn.Close()
		time.Sleep(300 * time.Second)
	})
}
