package main

import (
	"net"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/go-test-ethereum/pkg/valnode"
	"github.com/marcopolo/simnet"
)

// Same test but WITHOUT synctest — real wall clock time.
// Use to verify finality works before debugging synctest issues.
func TestEthereumRealTime(t *testing.T) {
	sn := &simnet.Simnet{
		LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
	}
	ls := simnet.NodeBiDiLinkSettings{
		Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
	}
	c1 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, ls)
	c2 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}, ls)
	sn.Start()
	defer sn.Close()

	gen := genesis.Generate(t, genesis.Config{
		NumValidators: 64,
		GenesisTime:   time.Now().Add(10 * time.Second),
	})

	el1 := elnode.Start(t, gen.ELGenesis)
	el2 := elnode.Start(t, gen.ELGenesis)

	o1, _, _ := quicnet.NewSimnetTransport(c1)
	o2, _, _ := quicnet.NewSimnetTransport(c2)
	bc1 := valnode.NewBufconnPair()
	bc2 := valnode.NewBufconnPair()

	cl1 := clnode.Start(t, clnode.Config{
		GenesisState: gen.CLState, BeaconConfig: gen.BeaconConfig,
		RPCClient: el1.Attach(), Libp2pOptions: o1,
		GRPCListener: bc1.GRPCListener, HTTPListener: bc1.HTTPListener,
	})
	cl2 := clnode.Start(t, clnode.Config{
		GenesisState: gen.CLState, BeaconConfig: gen.BeaconConfig,
		RPCClient: el2.Attach(), Libp2pOptions: o2,
		GRPCListener: bc2.GRPCListener, HTTPListener: bc2.HTTPListener,
	})

	time.Sleep(1 * time.Second)
	p2p1 := cl1.P2PService()
	p2p2 := cl2.P2PService()
	if p2p1 != nil && p2p2 != nil && p2p2.Host() != nil {
		p2p1.Connect(peer.AddrInfo{ID: p2p2.Host().ID(), Addrs: p2p2.Host().Addrs()})
	}

	valnode.Start(t, bc1, valnode.Config{NumValidators: 32, StartIndex: 0})
	valnode.Start(t, bc2, valnode.Config{NumValidators: 32, StartIndex: 32})

	// Wait for 4 epochs (128s each) + genesis delay
	time.Sleep(550 * time.Second)
	t.Log("Done waiting")
}
