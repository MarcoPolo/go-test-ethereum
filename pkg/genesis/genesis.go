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

	// Use mainnet config with faster timing for testing.
	// We can't use MinimalSpecConfig() because it requires the 'minimal' build tag
	// for SSZ field sizes (SlashingsLength, etc.) which don't compile cleanly.
	beaconCfg := params.MainnetConfig().Copy()
	// All forks active at genesis
	beaconCfg.AltairForkEpoch = 0
	beaconCfg.BellatrixForkEpoch = 0
	beaconCfg.CapellaForkEpoch = 0
	beaconCfg.DenebForkEpoch = 0
	beaconCfg.ElectraForkEpoch = 0
	beaconCfg.FuluForkEpoch = 0
	// Reduce timing for fast testing (keep SlotsPerEpoch=32 for mainnet field param compat)
	beaconCfg.SecondsPerSlot = 4
	beaconCfg.SlotDurationMilliseconds = 4000
	beaconCfg.GenesisDelay = 0
	beaconCfg.MinGenesisTime = uint64(cfg.GenesisTime.Unix())
	beaconCfg.MinGenesisActiveValidatorCount = cfg.NumValidators
	beaconCfg.ConfigName = "interop"
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
