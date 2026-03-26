package main

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/marcopolo/go-test-ethereum/pkg/clnode"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/go-test-ethereum/pkg/valnode"
	"github.com/marcopolo/simnet"
)

func TestSynctestELOnly(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})
		el := elnode.Start(t, gen.ELGenesis)
		t.Log("EL started")
		_ = el
		time.Sleep(20 * time.Second)
		t.Log("Sleep done")
		el.Stack.Close()
		time.Sleep(1 * time.Second)
	})
}

func TestSynctestOneCL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sn := &simnet.Simnet{}
		linkSettings := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, linkSettings)
		sn.Start()

		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})
		t.Log("Genesis generated")

		el := elnode.Start(t, gen.ELGenesis)
		t.Log("EL started")

		clOpts, _, err := quicnet.NewSimnetTransport(conn)
		if err != nil {
			t.Fatal(err)
		}

		bc := valnode.NewBufconnPair()

		cl := clnode.Start(t, clnode.Config{
			GenesisState:  gen.CLState,
			BeaconConfig:  gen.BeaconConfig,
			RPCClient:     el.Attach(),
			Libp2pOptions: clOpts,
			GRPCListener:  bc.GRPCListener,
			HTTPListener:  bc.HTTPListener,
		})
		t.Log("CL started")

		t.Log("Sleeping 60s (should cover genesis + a few slots)...")
		time.Sleep(60 * time.Second)
		t.Log("Sleep done — fake time advanced")

		cl.Close()
		el.Stack.Close()
		sn.Close()
		time.Sleep(2 * time.Second)
	})
}

func TestSynctestTwoCL(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sn := &simnet.Simnet{}
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		c1 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, ls)
		c2 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 9000}, ls)
		sn.Start()

		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		el1 := elnode.Start(t, gen.ELGenesis)
		el2 := elnode.Start(t, gen.ELGenesis)

		o1, st1, _ := quicnet.NewSimnetTransport(c1)
		o2, st2, _ := quicnet.NewSimnetTransport(c2)

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
		t.Log("Both CL nodes started")

		t.Log("Sleeping 60s...")
		time.Sleep(60 * time.Second)
		t.Log("Sleep done")

		cl1.Close()
		cl2.Close()
		time.Sleep(100 * time.Millisecond) // let goroutines process cancellation
		st1.ConnManager.Close()
		st2.ConnManager.Close()
		c1.Close()
		c2.Close()
		el1.Stack.Close()
		el2.Stack.Close()
		sn.Close()
		time.Sleep(300 * time.Second) // let remaining goroutines drain (a few slot durations)
	})
}
