package main

import (
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/simnet"
)

// Pure QUIC dial under synctest — works
func TestQUICSynctestDial(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
		}
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		conn1 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 30303}, ls)
		conn2 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 30303}, ls)
		sn.Start()

		tr1, _ := quicnet.NewELTransport(conn1)
		tr2, _ := quicnet.NewELTransport(conn2)

		lis := tr2.Listener()
		go func() {
			c, err := lis.Accept()
			if err != nil {
				return
			}
			c.Close()
		}()

		c, err := tr1.Dial(t.Context(), conn2.LocalAddr())
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		t.Logf("dial succeeded: %s -> %s", c.LocalAddr(), c.RemoteAddr())
		c.Close()

		tr1.Close()
		tr2.Close()
		conn1.Close()
		conn2.Close()
		sn.Close()
		time.Sleep(1 * time.Second)
	})
}

// Geth EL nodes under synctest — peer connection fails
func TestQUICSynctestGeth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fuluEra := time.Unix(1764798551, 0)
		time.Sleep(fuluEra.Sub(time.Now()))

		sn := &simnet.Simnet{
			LatencyFunc: simnet.StaticLatency(1 * time.Millisecond),
		}
		ls := simnet.NodeBiDiLinkSettings{
			Downlink: simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
			Uplink:   simnet.LinkSettings{BitsPerSecond: 100 * simnet.Mibps},
		}
		conn1 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.1"), Port: 30303}, ls)
		conn2 := sn.NewEndpoint(&net.UDPAddr{IP: net.ParseIP("1.0.0.2"), Port: 30303}, ls)
		sn.Start()

		gen := genesis.Generate(t, genesis.Config{
			NumValidators: 64,
			GenesisTime:   time.Now().Add(10 * time.Second),
		})

		tr1, _ := quicnet.NewELTransport(conn1)
		tr2, _ := quicnet.NewELTransport(conn2)

		el1 := elnode.Start(t, gen.ELGenesis, elnode.Config{
			ListenFunc: func(_, _ string) (net.Listener, error) { return tr1.Listener(), nil },
			Dialer:     &elnode.QUICDialer{DialFunc: tr1.Dial},
			ListenAddr: conn1.LocalAddr().String(),
		})
		el2 := elnode.Start(t, gen.ELGenesis, elnode.Config{
			ListenFunc: func(_, _ string) (net.Listener, error) { return tr2.Listener(), nil },
			Dialer:     &elnode.QUICDialer{DialFunc: tr2.Dial},
			ListenAddr: conn2.LocalAddr().String(),
		})

		el1.Stack.Server().LocalNode().SetStaticIP(net.ParseIP("1.0.0.1"))
		el2.Stack.Server().LocalNode().SetStaticIP(net.ParseIP("1.0.0.2"))

		// Wait for geth to fully start
		time.Sleep(2 * time.Second)

		// Try geth's AddPeer
		el1.Stack.Server().AddPeer(el2.Enode())

		for i := 0; i < 15; i++ {
			time.Sleep(1 * time.Second)
			c1 := el1.Stack.Server().PeerCount()
			if c1 > 0 {
				t.Logf("connected after %ds", i+1)
				break
			}
			if i == 14 {
				t.Logf("EL peers not connected after 15s")
			}
		}

		t.Logf("EL1 peers: %d, EL2 peers: %d",
			el1.Stack.Server().PeerCount(), el2.Stack.Server().PeerCount())

		el1.Stack.Close()
		el2.Stack.Close()
		tr1.Close()
		tr2.Close()
		conn1.Close()
		conn2.Close()
		core.SenderCacher().Close()
		sn.Close()
		time.Sleep(5 * time.Second)
	})
}
