package main

import (
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/marcopolo/go-test-ethereum/pkg/elnode"
	"github.com/marcopolo/go-test-ethereum/pkg/genesis"
	"github.com/marcopolo/go-test-ethereum/pkg/quicnet"
	"github.com/marcopolo/simnet"

	"github.com/ethereum/go-ethereum/core"
)

func TestELP2PManualDial(t *testing.T) {
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
			buf := make([]byte, 10)
			n, _ := c.Read(buf)
			t.Logf("Accepted read: %q", string(buf[:n]))
			c.Close()
		}()

		ctx := t.Context()
		dialConn, err := tr1.Dial(ctx, conn2.LocalAddr())
		if err != nil {
			t.Fatalf("dial failed: %v", err)
		}
		dialConn.Write([]byte("hello"))
		t.Log("Manual dial+write succeeded")
		dialConn.Close()

		tr1.Close()
		tr2.Close()
		conn1.Close()
		conn2.Close()
		sn.Close()
		time.Sleep(1 * time.Second)
	})
}

func TestELP2P(t *testing.T) {
	// Run WITHOUT synctest
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
	defer sn.Close()

	gen := genesis.Generate(t, genesis.Config{NumValidators: 64})

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

	// Check packet stats before
	s1 := conn1.Stats()
	s2 := conn2.Stats()
	t.Logf("Before AddPeer: conn1 sent=%d rcvd=%d, conn2 sent=%d rcvd=%d", s1.PacketsSent, s1.PacketsRcvd, s2.PacketsSent, s2.PacketsRcvd)

	time.Sleep(2 * time.Second)
	el1.Stack.Server().AddPeer(el2.Enode())
	t.Logf("AddPeer called: EL1 -> EL2 (%s)", el2.Enode().URLv4())

	for i := 0; i < 10; i++ {
		time.Sleep(1 * time.Second)
		s1 = conn1.Stats()
		s2 = conn2.Stats()
		c1 := el1.Stack.Server().PeerCount()
		c2 := el2.Stack.Server().PeerCount()
		t.Logf("t+%ds: peers=%d/%d, conn1(sent=%d,rcvd=%d) conn2(sent=%d,rcvd=%d)",
			i+1, c1, c2, s1.PacketsSent, s1.PacketsRcvd, s2.PacketsSent, s2.PacketsRcvd)
		if c1 > 0 {
			t.Log("Connected!")
			break
		}
	}

	if el1.Stack.Server().PeerCount() == 0 {
		// Try manual dial on the same transport to see if it works
		t.Log("Geth P2P failed. Trying manual dial on same transport...")
		ctx := t.Context()
		mc, err := tr1.Dial(ctx, conn2.LocalAddr())
		if err != nil {
			t.Logf("Manual dial also failed: %v", err)
		} else {
			t.Logf("Manual dial succeeded! local=%s remote=%s", mc.LocalAddr(), mc.RemoteAddr())
			mc.Close()
		}
		t.Fatal("EL peers not connected")
	}

	fmt.Println("EL P2P test passed")
	el1.Stack.Close()
	el2.Stack.Close()
	core.SenderCacher().Close()
}
