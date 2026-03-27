// Package genesis generates matching EL and CL genesis for in-process Ethereum testing.
package genesis

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/OffchainLabs/prysm/v7/beacon-chain/state"
	"github.com/OffchainLabs/prysm/v7/config/params"
	"github.com/OffchainLabs/prysm/v7/runtime/interop"
	"github.com/OffchainLabs/prysm/v7/runtime/version"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
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
	beaconCfg.DepositChainID = 1337
	beaconCfg.DepositNetworkID = 1337
	beaconCfg.ConfigName = "interop"
	beaconCfg.InitializeForkSchedule()
	params.OverrideBeaconConfig(beaconCfg)

	// Generate matching EL genesis
	elGenesis := interop.GethTestnetGenesis(cfg.GenesisTime, beaconCfg)

	// Fund the test transaction sender account
	// Key: b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291
	// Address: 0x71562b71999567a775a2b8cfcdf1512e04b0a9b4 (derived from key)
	testAddr := common.HexToAddress("0x71562b71999873DB5b286dF957af199Ec94617F7")
	elGenesis.Alloc[testAddr] = types.Account{
		Balance: new(big.Int).Mul(big.NewInt(1e18), big.NewInt(1_000_000)), // 1M ETH
	}

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
