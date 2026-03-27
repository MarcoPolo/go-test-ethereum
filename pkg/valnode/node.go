// Package valnode wraps Prysm's validator client for in-process usage.
package valnode

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sync"
	"testing"

	grpcutil "github.com/OffchainLabs/prysm/v7/api/grpc"
	"github.com/OffchainLabs/prysm/v7/api/rest"
	g "github.com/OffchainLabs/prysm/v7/validator/graffiti"
	"github.com/OffchainLabs/prysm/v7/validator/client"
	"github.com/OffchainLabs/prysm/v7/validator/db/filesystem"
	validatorHelpers "github.com/OffchainLabs/prysm/v7/validator/helpers"
	"github.com/OffchainLabs/prysm/v7/validator/keymanager/local"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

// Config holds the configuration for creating a validator node.
type Config struct {
	NumValidators uint64
	StartIndex    uint64
	Indices       []uint64 // If set, use these specific validator indices (overrides StartIndex/NumValidators)
	DataDir       string
}

// BufconnPair holds bufconn listeners for both gRPC and HTTP REST.
type BufconnPair struct {
	GRPCListener *bufconn.Listener
	HTTPListener *bufconn.Listener
}

// NewBufconnPair creates bufconn listeners for in-process gRPC and HTTP.
func NewBufconnPair() *BufconnPair {
	return &BufconnPair{
		GRPCListener: bufconn.Listen(bufSize),
		HTTPListener: bufconn.Listen(bufSize),
	}
}

// bufconnGRPCProvider implements grpcutil.GrpcConnectionProvider using bufconn.
type bufconnGRPCProvider struct {
	lis     *bufconn.Listener
	conn    *grpc.ClientConn
	mu      sync.Mutex
	closed  bool
	counter uint64
}

func newBufconnGRPCConn(lis *bufconn.Listener) *grpc.ClientConn {
	maxMsgSize := 10 * 1024 * 1024
	//nolint:staticcheck // grpc.Dial is deprecated but grpc.NewClient doesn't apply DefaultCallOptions correctly
	conn, err := grpc.Dial(
		"bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(maxMsgSize),
			grpc.MaxCallSendMsgSize(maxMsgSize),
		),
	)
	if err != nil {
		panic(err)
	}
	return conn
}

func (p *bufconnGRPCProvider) CurrentConn() *grpc.ClientConn {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	return p.conn
}

func (p *bufconnGRPCProvider) CurrentHost() string       { return "bufconn" }
func (p *bufconnGRPCProvider) Hosts() []string            { return []string{"bufconn"} }
func (p *bufconnGRPCProvider) SwitchHost(_ int) error     { return nil }
func (p *bufconnGRPCProvider) ConnectionCounter() uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.counter
}
func (p *bufconnGRPCProvider) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.conn != nil {
		p.conn.Close()
	}
}

var _ grpcutil.GrpcConnectionProvider = (*bufconnGRPCProvider)(nil)

// bufconnRESTProvider implements rest.RestConnectionProvider using bufconn HTTP.
type bufconnRESTProvider struct {
	lis        *bufconn.Listener
	httpClient *http.Client
}

func newBufconnRESTProvider(lis *bufconn.Listener) *bufconnRESTProvider {
	return &bufconnRESTProvider{
		lis: lis,
		httpClient: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return lis.DialContext(ctx)
				},
			},
		},
	}
}

func (p *bufconnRESTProvider) Hosts() []string          { return []string{"http://bufconn"} }
func (p *bufconnRESTProvider) CurrentHost() string      { return "http://bufconn" }
func (p *bufconnRESTProvider) SwitchHost(_ int) error   { return nil }
func (p *bufconnRESTProvider) HttpClient() *http.Client { return p.httpClient }
func (p *bufconnRESTProvider) Handler() rest.Handler {
	return rest.NewHandler(*p.httpClient, "http://bufconn")
}

var _ rest.RestConnectionProvider = (*bufconnRESTProvider)(nil)

// ValNode wraps a running validator service with a cleanup function.
type ValNode struct {
	cancel   context.CancelFunc
	grpcProv *bufconnGRPCProvider
	svc      *client.ValidatorService
}

// Close shuts down the validator node.
func (v *ValNode) Close() {
	v.cancel()
	if v.svc != nil {
		v.svc.Stop()
	}
	v.grpcProv.Close()
}

// Start creates and starts a Prysm validator client connected to a beacon node via bufconn.
func Start(t *testing.T, bc *BufconnPair, cfg Config) *ValNode {
	t.Helper()

	if cfg.DataDir == "" {
		cfg.DataDir = t.TempDir()
	}

	grpcProv := &bufconnGRPCProvider{
		lis:  bc.GRPCListener,
		conn: newBufconnGRPCConn(bc.GRPCListener),
	}
	restProv := newBufconnRESTProvider(bc.HTTPListener)

	nodeConn, err := validatorHelpers.NewNodeConnection(
		validatorHelpers.WithGRPCProvider(grpcProv),
		validatorHelpers.WithRestProvider(restProv),
	)
	if err != nil {
		t.Fatalf("failed to create validator node connection: %v", err)
	}

	valDB, err := filesystem.NewStore(cfg.DataDir, nil)
	if err != nil {
		t.Fatalf("failed to create validator database: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	kmConfig := &local.InteropKeymanagerConfig{
		Offset:           cfg.StartIndex,
		NumValidatorKeys: cfg.NumValidators,
	}
	if len(cfg.Indices) > 0 {
		kmConfig = &local.InteropKeymanagerConfig{Indices: cfg.Indices}
	}

	validatorService, err := client.NewValidatorService(ctx, &client.Config{
		DB:                     valDB,
		Conn:                   nodeConn,
		GRPCMaxCallRecvMsgSize: 10 * 1024 * 1024, // 10MB
		InteropKmConfig:        kmConfig,
		GraffitiStruct:          &g.Graffiti{},
		LogValidatorPerformance: true,
		CloseClientFunc:         func() { cancel() },
	})
	if err != nil {
		t.Fatalf("failed to create validator service: %v", err)
	}

	go validatorService.Start()
	if len(cfg.Indices) > 0 {
		t.Logf("Validator service started (%d validators, round-robin indices)", len(cfg.Indices))
	} else {
		t.Log(fmt.Sprintf("Validator service started (indices %d-%d)", cfg.StartIndex, cfg.StartIndex+cfg.NumValidators-1))
	}

	return &ValNode{
		cancel:   cancel,
		grpcProv: grpcProv,
		svc:      validatorService,
	}
}
