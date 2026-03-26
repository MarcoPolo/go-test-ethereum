// Package genesis generates matching EL and CL genesis for in-process Ethereum testing.
package genesis

import (
	"context"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/OffchainLabs/prysm/v7/runtime/interop"
	"github.com/ethereum/go-ethereum/core"

	ethpb "github.com/OffchainLabs/prysm/v7/proto/prysm/v1alpha1"
)

// Config holds the genesis configuration.
type Config struct {
	NumValidators uint64
	GenesisTime   time.Time
}

// Result holds the generated genesis data for both layers.
type Result struct {
	ELGenesis    *core.Genesis
	CLState      *ethpb.BeaconState
	CLDeposits   []*ethpb.Deposit
	GenesisTime  time.Time
	BeaconConfig *params.BeaconChainConfig
}

// Generate creates matching EL and CL genesis states using Prysm's minimal spec config.
func Generate(t *testing.T, cfg Config) *Result {
	t.Helper()

	if cfg.NumValidators == 0 {
		cfg.NumValidators = 64
	}
	if cfg.GenesisTime.IsZero() {
		cfg.GenesisTime = time.Now()
	}

	// Use minimal spec config for fast epochs (8 slots/epoch, 6s slots)
	beaconCfg := params.MinimalSpecConfig()
	// All forks active at genesis
	beaconCfg.AltairForkEpoch = 0
	beaconCfg.BellatrixForkEpoch = 0
	beaconCfg.CapellaForkEpoch = 0
	beaconCfg.DenebForkEpoch = 0
	beaconCfg.ElectraForkEpoch = 0
	// Reduce genesis delay for testing
	beaconCfg.GenesisDelay = 0
	params.OverrideBeaconConfig(beaconCfg)

	// Generate CL genesis state with deterministic validator keys
	genesisTimeUnix := uint64(cfg.GenesisTime.Unix())
	clState, deposits, err := interop.GenerateGenesisState(
		context.Background(),
		genesisTimeUnix,
		cfg.NumValidators,
	)
	if err != nil {
		t.Fatalf("failed to generate CL genesis state: %v", err)
	}

	// Generate matching EL genesis
	elGenesis := interop.GethTestnetGenesis(cfg.GenesisTime, beaconCfg)

	return &Result{
		ELGenesis:    elGenesis,
		CLState:      clState,
		CLDeposits:   deposits,
		GenesisTime:  cfg.GenesisTime,
		BeaconConfig: beaconCfg,
	}
}
