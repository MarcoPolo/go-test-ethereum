package quicnet

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"math/big"
	"net"
	"time"

	"github.com/marcopolo/simnet"
	"github.com/quic-go/quic-go"
)

// QUICStreamListener implements net.Listener using QUIC streams over simnet.
type QUICStreamListener struct {
	ql   *quic.Listener
	addr net.Addr
}

// NewQUICStreamListener creates a net.Listener backed by QUIC over a simnet connection.
func NewQUICStreamListener(conn *simnet.SimConn) (*QUICStreamListener, error) {
	tr := &quic.Transport{Conn: conn}
	ql, err := tr.Listen(generateTLSConfig(), &quic.Config{})
	if err != nil {
		return nil, fmt.Errorf("quic listen: %w", err)
	}
	return &QUICStreamListener{ql: ql, addr: conn.LocalAddr()}, nil
}

func (l *QUICStreamListener) Accept() (net.Conn, error) {
	qconn, err := l.ql.Accept(context.Background())
	if err != nil {
		return nil, err
	}
	stream, err := qconn.AcceptStream(context.Background())
	if err != nil {
		return nil, err
	}
	return &quicStreamConn{Stream: stream, local: l.addr, remote: qconn.RemoteAddr()}, nil
}

func (l *QUICStreamListener) Close() error   { return l.ql.Close() }
func (l *QUICStreamListener) Addr() net.Addr { return l.addr }

// DialQUICStream dials a QUIC connection and opens a stream, returning a net.Conn.
func DialQUICStream(ctx context.Context, conn *simnet.SimConn, remoteAddr net.Addr) (net.Conn, error) {
	tr := &quic.Transport{Conn: conn}
	tlsConf := &tls.Config{InsecureSkipVerify: true, NextProtos: []string{"devp2p"}}
	qconn, err := tr.Dial(ctx, remoteAddr, tlsConf, &quic.Config{})
	if err != nil {
		return nil, fmt.Errorf("quic dial: %w", err)
	}
	stream, err := qconn.OpenStreamSync(ctx)
	if err != nil {
		return nil, fmt.Errorf("open stream: %w", err)
	}
	return &quicStreamConn{Stream: stream, local: conn.LocalAddr(), remote: remoteAddr}, nil
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
