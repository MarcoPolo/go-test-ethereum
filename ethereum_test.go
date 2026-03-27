package main

import (
	"context"
	"fmt"
	"math/big"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/sirupsen/logrus"
	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/go-test-ethereum/pkg/txspam"
	"github.com/marcopolo/go-test-ethereum/pkg/valnode"
	"github.com/marcopolo/simnet"
)

func TestEthereum(t *testing.T) {
	const numNodes = 3
	const numValidators = 64

	// Use full timestamps in logrus output. The default relative timestamp
	// (seconds since process start) is meaningless under synctest because
	// the fake clock and the real clock used at package init diverge.
	if f, ok := logrus.StandardLogger().Formatter.(*logrus.TextFormatter); ok {
		f.FullTimestamp = true
	}

	synctest.Test(t, func(t *testing.T) {
		// 1. Setup simnet
		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(50 * time.Millisecond),
		}
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}

		// Create simnet endpoints: CL (port 9000) and EL (port 30303) per node
		type simEndpoints struct {
			cl *simnet.SimConn
			el *simnet.SimConn
		}
		endpoints := make([]simEndpoints, numNodes)
		for i := range endpoints {
			ip := net.ParseIP(fmt.Sprintf("1.0.0.%d", i+1))
			endpoints[i] = simEndpoints{
				cl: sn.NewEndpoint(&net.UDPAddr{IP: ip, Port: 9000}, ls),
				el: sn.NewEndpoint(&net.UDPAddr{IP: ip, Port: 30303}, ls),
			}
		}
		sn.Start()

		// 2. Advance synctest clock to a realistic Fulu-era time.
		// synctest starts at 2000-01-01. Sleep to jump to ~2025-12-03.
		fuluEra := time.Unix(1764798551, 0)
		time.Sleep(fuluEra.Sub(time.Now()))

		// 3. Generate genesis (10s after current fake time)
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: numValidators,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		// 3. Create EL transports (shared QUIC transport per node for listen + dial)
		elTransports := make([]*quicnet.ELTransport, numNodes)
		for i := range elTransports {
			var err error
			elTransports[i], err = quicnet.NewELTransport(endpoints[i].el)
			if err != nil {
				t.Fatalf("EL transport %d: %v", i, err)
			}
		}

		// 4. Start EL nodes with QUIC P2P
		els := make([]*elnode.Node, numNodes)
		for i := range els {
			elTr := elTransports[i]
			lis := elTr.Listener()
			els[i] = elnode.Start(t, gen.ELGenesis, elnode.Config{
				ListenFunc: func(_, _ string) (net.Listener, error) { return lis, nil },
				Dialer: &elnode.QUICDialer{DialFunc: elTr.Dial},
				ListenAddr: endpoints[i].el.LocalAddr().String(),
			})
		}

		// Set correct IP on each EL's enode so peers can find each other.
		for i, el := range els {
			udpAddr := endpoints[i].el.LocalAddr().(*net.UDPAddr)
			el.Stack.Server().LocalNode().SetStaticIP(udpAddr.IP)
		}

		// Connect EL peers (full mesh)
		time.Sleep(100 * time.Millisecond)
		for i, el := range els {
			t.Logf("EL%d enode: %s  listen: %s", i, el.Enode().URLv4(), endpoints[i].el.LocalAddr())
		}
		// Connect as full mesh — smaller IP dials larger to avoid DiscAlreadyConnected race
		for i := range els {
			for j := i + 1; j < len(els); j++ {
				els[i].Stack.Server().AddPeer(els[j].Enode())
			}
		}
		time.Sleep(5 * time.Second)
		for i, el := range els {
			t.Logf("EL%d peer count: %d", i, el.Stack.Server().PeerCount())
		}

		// 5. Create CL QUIC transports
		clOpts := make([][]libp2p.Option, numNodes)
		clTransports := make([]*quicnet.SimnetTransport, numNodes)
		for i := range clOpts {
			var err error
			clOpts[i], clTransports[i], err = quicnet.NewSimnetTransport(endpoints[i].cl)
			if err != nil {
				t.Fatalf("CL transport %d: %v", i, err)
			}
		}

		// 6. Start CL nodes
		bcs := make([]*valnode.BufconnPair, numNodes)
		cls := make([]*clnode.Node, numNodes)
		for i := range cls {
			bcs[i] = valnode.NewBufconnPair()
			cls[i] = clnode.Start(t, clnode.Config{
				GenesisState: gen.CLState, BeaconConfig: gen.BeaconConfig,
				RPCClient: els[i].Attach(), Libp2pOptions: clOpts[i],
				GRPCListener: bcs[i].GRPCListener, HTTPListener: bcs[i].HTTPListener,
			})
		}

		// 7. Connect CL peers (full mesh)
		time.Sleep(1 * time.Second)
		for i := range cls {
			for j := range cls {
				if j > i {
					p2pi := cls[i].P2PService()
					p2pj := cls[j].P2PService()
					if err := p2pi.Connect(peer.AddrInfo{ID: p2pj.Host().ID(), Addrs: p2pj.Host().Addrs()}); err != nil {
						t.Logf("CL%d->CL%d connect: %v", i, j, err)
					}
				}
			}
		}
		t.Log("All CL peers connected")

		// 8. Start validators — round-robin across all CL nodes
		nodeIndices := make([][]uint64, numNodes)
		for v := 0; v < numValidators; v++ {
			node := v % numNodes
			nodeIndices[node] = append(nodeIndices[node], uint64(v))
		}
		vals := make([]*valnode.ValNode, numNodes)
		for i := range vals {
			vals[i] = valnode.Start(t, bcs[i], valnode.Config{
				Indices: nodeIndices[i],
			})
		}

		// 9. Wait for genesis + a few slots, then start tx spammer
		time.Sleep(20 * time.Second) // past genesis (T+10) + a couple slots
		spamCtx, spamCancel := context.WithCancel(context.Background())
		spamRPC := els[0].Attach()
		txspam.Start(spamCtx, t, spamRPC, big.NewInt(1337), 4*time.Second)
		t.Log("Transaction spammer started")

		// 10. Wait for finality
		t.Log("Waiting for finality...")
		time.Sleep(800 * time.Second)

		// 10. Assert all nodes agree on finalized epoch
		epochs := make([]uint64, numNodes)
		for i, cl := range cls {
			epochs[i] = cl.FinalizedEpoch()
			t.Logf("Node %d finalized epoch: %d", i, epochs[i])
		}
		for i := range epochs {
			if epochs[i] < 2 {
				t.Fatalf("node %d: expected finalized epoch >= 2, got %d", i, epochs[i])
			}
			if epochs[i] != epochs[0] {
				t.Fatalf("nodes disagree: node 0 epoch %d, node %d epoch %d", epochs[0], i, epochs[i])
			}
		}
		t.Logf("SUCCESS: All %d nodes agree on finalized epoch %d", numNodes, epochs[0])

		// Shutdown
		spamCancel()
		spamRPC.Close()
		for _, v := range vals {
			v.Close()
		}
		for _, cl := range cls {
			cl.Close()
		}
		for _, st := range clTransports {
			st.ConnManager.Close()
		}
		for _, ep := range endpoints {
			ep.cl.Close()
			ep.el.Close()
		}
		for _, tr := range elTransports {
			tr.Close()
		}
		for _, el := range els {
			el.Stack.Close()
		}
		core.SenderCacher().Close()
		sn.Close()
		time.Sleep(30 * time.Second)
	})
}
