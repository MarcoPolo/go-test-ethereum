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

func TestEthereum(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		// 1. Setup simnet (needed for libp2p QUIC transport)
		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
		}
		conn := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 9000}, simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		})
		sn.Start()

		// 2. Generate genesis
		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		// 3. Start EL node
		el := elnode.Start(t, gen.ELGenesis)

		// 4. Create QUIC transport
		clOpts, st, err := quicnet.NewSimnetTransport(conn)
		if err != nil {
			t.Fatal(err)
		}

		// 5. Start CL node with all 64 validators
		bc := valnode.NewBufconnPair()
		cl := clnode.Start(t, clnode.Config{
			GenesisState:  gen.CLState,
			BeaconConfig:  gen.BeaconConfig,
			RPCClient:     el.Attach(),
			Libp2pOptions: clOpts,
			GRPCListener:  bc.GRPCListener,
			HTTPListener:  bc.HTTPListener,
		})

		// 6. Start validator with all 64 keys
		v := valnode.Start(t, bc, valnode.Config{
			NumValidators: 64,
			StartIndex:    0,
		})

		// 7. Wait for finality (6 epochs = ~768s with 4s slots)
		t.Log("Waiting for finality...")
		time.Sleep(800 * time.Second)
		t.Log("Done waiting")

		// Shutdown
		v.Close()
		cl.Close()
		st.ConnManager.Close()
		conn.Close()
		el.Stack.Close()
		sn.Close()
		time.Sleep(300 * time.Second)
	})
}
