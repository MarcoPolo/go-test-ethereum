# go-test-ethereum

A single `go test` that runs a multi-node Ethereum PoS network (Prysm CL + go-ethereum EL) inside one Go process. Uses `testing/synctest` for deterministic fake time, `simnet` for simulated networking, and QUIC for all transport.

## Setup

Requires Go 1.26+.

```bash
git clone <repo-url>
cd go-test-ethereum
git submodule update --init
./patches/apply.sh
```

The `patches/apply.sh` script checks out the upstream base commits for `go-ethereum` and `prysm`, then applies the local patches needed for in-process testing and synctest compatibility.

## Run

```bash
go test -v -run TestEthereum -timeout 120s -count=1 .
```
