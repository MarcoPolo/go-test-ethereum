package quicnet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"log"
	"math/big"
	"net"
	"time"

	"github.com/marcopolo/simnet"
	"github.com/quic-go/quic-go"
)

// ELTransport provides both listener and dialer over a single QUIC transport on simnet.
type ELTransport struct {
	tr  *quic.Transport
	ql  *quic.Listener
	tls *tls.Config
	addr net.Addr
}

// NewELTransport creates a shared QUIC transport for EL P2P over a simnet connection.
func NewELTransport(conn *simnet.SimConn) (*ELTransport, error) {
	tlsConf := generateTLSConfig()
	tr := &quic.Transport{Conn: conn}
	ql, err := tr.Listen(tlsConf, &quic.Config{})
	if err != nil {
		return nil, err
	}
	return &ELTransport{tr: tr, ql: ql, tls: tlsConf, addr: conn.LocalAddr()}, nil
}

// Listener returns a net.Listener backed by QUIC streams.
func (t *ELTransport) Listener() net.Listener {
	return &quicStreamListener{ql: t.ql, addr: t.addr}
}

// Dial opens a QUIC stream to the given address, returning a net.Conn.
func (t *ELTransport) Dial(ctx context.Context, addr net.Addr) (net.Conn, error) {
	tlsConf := &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"devp2p"}}
	qconn, err := t.tr.Dial(ctx, addr, tlsConf, &quic.Config{})
	if err != nil {
		return nil, err
	}
	stream, err := qconn.OpenStreamSync(ctx)
	if err != nil {
		return nil, err
	}
	return &quicStreamConn{Stream: stream, local: t.addr, remote: addr}, nil
}

func (t *ELTransport) Close() error { return t.ql.Close() }

// quicStreamListener implements net.Listener using QUIC streams.
type quicStreamListener struct {
	ql   *quic.Listener
	addr net.Addr
}

func (l *quicStreamListener) Accept() (net.Conn, error) {
	log.Printf("quicStreamListener.Accept: waiting for connection on %s", l.addr)
	qconn, err := l.ql.Accept(context.Background())
	if err != nil {
		log.Printf("quicStreamListener.Accept: ql.Accept error: %v", err)
		return nil, err
	}
	log.Printf("quicStreamListener.Accept: got QUIC conn from %s", qconn.RemoteAddr())
	stream, err := qconn.AcceptStream(context.Background())
	if err != nil {
		log.Printf("quicStreamListener.Accept: AcceptStream error: %v", err)
		return nil, err
	}
	log.Printf("quicStreamListener.Accept: got stream from %s", qconn.RemoteAddr())
	return &quicStreamConn{Stream: stream, local: l.addr, remote: qconn.RemoteAddr()}, nil
}

func (l *quicStreamListener) Close() error { return l.ql.Close() }
func (l *quicStreamListener) Addr() net.Addr {
	udp, ok := l.addr.(*net.UDPAddr)
	if ok {
		return &net.TCPAddr{IP: udp.IP, Port: udp.Port}
	}
	return l.addr
}

// quicStreamConn wraps a *quic.Stream to implement net.Conn.
type quicStreamConn struct {
	*quic.Stream
	local  net.Addr
	remote net.Addr
}

func (c *quicStreamConn) LocalAddr() net.Addr  { return c.local }
func (c *quicStreamConn) RemoteAddr() net.Addr { return c.remote }

func generateTLSConfig() *tls.Config {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-1 * time.Hour),
		NotAfter:     time.Now().Add(24 * 365 * time.Hour),
	}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{certDER},
			PrivateKey:  key,
		}},
		NextProtos:         []string{"devp2p"},
		InsecureSkipVerify: true,
	}
}
