// Package quicnet provides helpers to create libp2p QUIC transports
// backed by simnet's simulated packet connections.
package quicnet

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"fmt"
	"net"

	"github.com/libp2p/go-libp2p"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/quicreuse"
	"github.com/marcopolo/simnet"
	ma "github.com/multiformats/go-multiaddr"
	"github.com/quic-go/quic-go"
)

// SimnetTransport holds the simnet connection and associated QUIC resources.
type SimnetTransport struct {
	SimConn     *simnet.SimConn
	ConnManager *quicreuse.ConnManager
	DoneCh      <-chan struct{}
}

// wrappedQUICTransport wraps *quic.Transport to satisfy quicreuse.QUICTransport.
// The Listen method returns QUICListener instead of *quic.Listener.
type wrappedQUICTransport struct {
	tr *quic.Transport
}

func (w *wrappedQUICTransport) Listen(tlsConf *tls.Config, conf *quic.Config) (quicreuse.QUICListener, error) {
	return w.tr.Listen(tlsConf, conf)
}

func (w *wrappedQUICTransport) Dial(ctx context.Context, addr net.Addr, tlsConf *tls.Config, conf *quic.Config) (quic.Connection, error) {
	return w.tr.Dial(ctx, addr, tlsConf, conf)
}

func (w *wrappedQUICTransport) WriteTo(b []byte, addr net.Addr) (int, error) {
	return w.tr.WriteTo(b, addr)
}

func (w *wrappedQUICTransport) ReadNonQUICPacket(ctx context.Context, b []byte) (int, net.Addr, error) {
	return w.tr.ReadNonQUICPacket(ctx, b)
}

func (w *wrappedQUICTransport) Close() error {
	return w.tr.Close()
}

// NewSimnetTransport creates a libp2p-compatible QUIC transport from a simnet endpoint.
// It returns libp2p options that can be passed to Config.CustomLibp2pOptions.
func NewSimnetTransport(conn *simnet.SimConn) ([]libp2p.Option, *SimnetTransport, error) {
	// Generate random keys for QUIC
	var srk quic.StatelessResetKey
	var tokenKey quic.TokenGeneratorKey
	if _, err := rand.Read(srk[:]); err != nil {
		return nil, nil, fmt.Errorf("failed to generate stateless reset key: %w", err)
	}
	if _, err := rand.Read(tokenKey[:]); err != nil {
		return nil, nil, fmt.Errorf("failed to generate token key: %w", err)
	}

	// Create a ConnManager with reuseport enabled (needed for LendTransport)
	cm, err := quicreuse.NewConnManager(srk, tokenKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create ConnManager: %w", err)
	}

	// Create a quic.Transport from the simnet PacketConn
	qtr := &quic.Transport{
		Conn:              conn,
		StatelessResetKey: &srk,
		TokenGeneratorKey: &tokenKey,
	}

	// Wrap to satisfy QUICTransport interface
	wrapped := &wrappedQUICTransport{tr: qtr}

	// LendTransport requires the local addr IP to be unspecified (0.0.0.0).
	// Save the real addr for the multiaddr, then temporarily set local addr to 0.0.0.0.
	realAddr := conn.LocalAddr().(*net.UDPAddr)
	unspecAddr := &net.UDPAddr{IP: net.IPv4zero, Port: realAddr.Port}
	conn.SetLocalAddr(unspecAddr)

	// Lend the transport to the ConnManager
	doneCh, err := cm.LendTransport("udp4", wrapped, conn)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to lend transport: %w", err)
	}

	// Restore real addr for actual routing
	conn.SetLocalAddr(realAddr)

	// Build the multiaddr for listening using the unspecified IP
	// (libp2p's quicreuse expects to match on 0.0.0.0:port)
	listenAddr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", realAddr.Port))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create multiaddr: %w", err)
	}

	// Return libp2p options that use this ConnManager and QUIC transport
	opts := []libp2p.Option{
		libp2p.QUICReuse(func(_ quic.StatelessResetKey, _ quic.TokenGeneratorKey, _ ...quicreuse.Option) (*quicreuse.ConnManager, error) {
			return cm, nil
		}),
		libp2p.Transport(libp2pquic.NewTransport),
		libp2p.ListenAddrs(listenAddr),
	}

	st := &SimnetTransport{
		SimConn:     conn,
		ConnManager: cm,
		DoneCh:      doneCh,
	}

	return opts, st, nil
}
