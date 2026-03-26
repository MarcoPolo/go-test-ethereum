// Package genesis generates matching EL and CL genesis for in-process Ethereum testing.
package genesis

import (
	"context"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/OffchainLabs/prysm/v7/runtime/interop"
	"github.com/OffchainLabs/prysm/v7/runtime/version"
	"github.com/ethereum/go-ethereum/core"
)

// Config holds the genesis configuration.
type Config struct {
	NumValidators uint64
	GenesisTime   time.Time
}

// Result holds the generated genesis data for both layers.
type Result struct {
	ELGenesis    *core.Genesis
	CLState      state.BeaconState
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
	beaconCfg.FuluForkEpoch = 0
	// Reduce genesis delay for testing
	beaconCfg.GenesisDelay = 0
	beaconCfg.MinGenesisTime = uint64(cfg.GenesisTime.Unix())
	beaconCfg.ConfigName = "minimal"
	beaconCfg.InitializeForkSchedule()
	params.OverrideBeaconConfig(beaconCfg)

	// Generate matching EL genesis
	elGenesis := interop.GethTestnetGenesis(cfg.GenesisTime, beaconCfg)

	// Get the genesis block
	genesisBlock := elGenesis.ToBlock()

	// Generate CL genesis state at Fulu fork using NewPreminedGenesis
	clState, err := interop.NewPreminedGenesis(
		context.Background(),
		cfg.GenesisTime,
		cfg.NumValidators,
		cfg.NumValidators, // all validators get execution credentials
		version.Fulu,
		genesisBlock,
	)
	if err != nil {
		t.Fatalf("failed to generate CL genesis state: %v", err)
	}

	return &Result{
		ELGenesis:    elGenesis,
		CLState:      clState,
		GenesisTime:  cfg.GenesisTime,
		BeaconConfig: beaconCfg,
	}
}
