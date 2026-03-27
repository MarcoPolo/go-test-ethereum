# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this project is

A single `go test` that runs a multi-node Ethereum PoS network (Prysm consensus + go-ethereum execution) inside one Go process. Uses `testing/synctest` for deterministic fake time, `simnet` for simulated networking, and QUIC for all transport (zero TCP).

The test starts 3 CL + 3 EL nodes, 64 validators, a transaction spammer (including blob txs), waits for consensus finality, and asserts all nodes agree on the finalized epoch.

## Commands

```bash
# Run the main test (needs -count=1 to avoid caching, -timeout for cleanup)
go test -v -run TestEthereum -timeout 120s -count=1 .

# Build all packages (does not compile test files)
go build ./...

# Build and verify test compiles
go test -c -o /dev/null .
```

The test takes ~100s wall clock (800s fake time via synctest). It requires Go 1.26+ for `testing/synctest`.

## Architecture

### Networking stack

All networking flows through simnet (simulated UDP packets). No real TCP/UDP sockets are used for node-to-node communication.

```
simnet (simulated packet network)
  ├── CL P2P: libp2p QUIC via quicreuse.ConnManager.LendTransport()
  ├── EL P2P: TCP-over-QUIC streams (quic.Stream implements net.Conn)
  └── Engine API: in-process RPC via geth's node.Attach() (no network)

Validator → Beacon: bufconn (in-memory) for both gRPC and HTTP REST
```

### Package roles

- **`pkg/genesis`** — Generates matching EL+CL genesis at Fulu fork. Mainnet field params with 4s slots. Funds a test account for tx spam.
- **`pkg/elnode`** — Wraps geth's `node.Node` + `eth.Ethereum`. In-memory DB, injectable `ListenFunc`/`Dialer` for QUIC P2P. Registers Engine API without heartbeat goroutine.
- **`pkg/clnode`** — Wraps Prysm's `BeaconNode` via synthetic `cli.Context`. Injects custom libp2p options, gRPC/HTTP listeners, RPC client, and beacon config.
- **`pkg/quicnet`** — Two transport adapters: `NewSimnetTransport` (CL libp2p QUIC) and `ELTransport` (EL TCP-over-QUIC streams). Also wraps `quic.Transport` to satisfy `quicreuse.QUICTransport` interface.
- **`pkg/valnode`** — Validator client connected via `bufconn`. Implements `GrpcConnectionProvider` and `RestConnectionProvider` over in-memory buffers.
- **`pkg/txspam`** — Sends ETH transfers and EIP-4844 blob txs (with PeerDAS cell proofs) to the EL via in-process RPC. Queries `PendingNonceAt` to avoid nonce gaps.

### Submodules (patched)

All three are git submodules with local patches committed as git commits:

- **`submodules/go-ethereum`** — Exported `ListenFunc` on P2P server, `NewConsensusAPIWithoutHeartbeat`, `txSenderCacher.Close()`, `LocalNode.Node()` sleep-under-lock fix.
- **`submodules/prysm`** — ~20 patches for library usage and synctest compatibility. Key ones: `WithRPCClient`, `CustomLibp2pOptions`, `WithP2PConfig`, `WithGRPCListener`/`WithHTTPListener`, `WithSkipSignalHandler`, `sync.Cond` in caches, goroutine leak fixes, libp2p v0.48 upgrade.
- **`submodules/simnet`** — Unpatched.

### synctest compatibility

`testing/synctest` requires all goroutines in the bubble to eventually exit. Key patterns that break synctest and their fixes in this codebase:

- **Mutex held during sleep** — synctest won't advance time if a goroutine waits on a mutex whose holder is sleeping. Fix: release mutex before sleeping (geth `LocalNode.Node()`), or use `sync.Cond` (Prysm cache `checkInProgress`).
- **Channel send with no reader** — goroutine blocks on `ch <- value` after consumer exits. Fix: select on done channel too (Prysm `SlotTicker`).
- **Background goroutines with no stop** — go-cache janitors, pebble, ristretto, catalyst heartbeat. Fix: disable janitors (cleanup interval=0), use in-memory DB, use `NewConsensusAPIWithoutHeartbeat`, close ristretto cache explicitly.
- **Cross-bubble channel access** — `signal.Notify` or `event.Feed` created inside bubble but accessed from outside. Fix: skip signal handler, disable `StreamServer`.
- **`context.Background()` in long-lived goroutines** — never cancels. Fix: pass cancellable context (DataColumnStorage).

### Key config values

- Chain ID: 1337
- Slots per epoch: 32 (mainnet)
- Seconds per slot: 4
- Genesis delay: 0
- Fork: Fulu (all prior forks active at epoch 0)
- Validators: 64 (interop deterministic keys)
- Test account key: `b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291`

## Known issues

1. **Process hangs after PASS** — `go-libp2p-pubsub` validation workers started at package init time don't stop. Use `-timeout` flag. The test itself passes (`--- PASS`).
2. **EL P2P peer count = 0** — QUIC transport is wired but devp2p handshake doesn't complete under synctest. Not blocking: CL gossip drives block sync via Engine API `newPayload`.
3. **Single proposer node** — All 64 validators on CL0. Splitting across nodes causes conflicting payload requests to separate ELs.
