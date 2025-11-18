package beaconchain

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/http"
	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/altair"
	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/attestantio/go-eth2-client/spec/deneb"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/sirupsen/logrus"

	"github.com/ethpandaops/eth-beacon-genesis/beaconconfig"
	"github.com/ethpandaops/eth-beacon-genesis/beaconutils"
	"github.com/ethpandaops/eth-beacon-genesis/validators"
	dynssz "github.com/pk910/dynamic-ssz"
)

type electraBuilder struct {
	elGenesis       *core.Genesis
	clConfig        *beaconconfig.Config
	dynSsz          *dynssz.DynSsz
	shadowForkBlock *types.Block
	validators      []*validators.Validator
}

func NewElectraBuilder(elGenesis *core.Genesis, clConfig *beaconconfig.Config) BeaconGenesisBuilder {
	return &electraBuilder{
		elGenesis: elGenesis,
		clConfig:  clConfig,
		dynSsz:    beaconutils.GetDynSSZ(clConfig),
	}
}

func (b *electraBuilder) SetShadowForkBlock(block *types.Block) {
	b.shadowForkBlock = block
}

func (b *electraBuilder) AddValidators(val []*validators.Validator) {
	b.validators = append(b.validators, val...)
}

func (b *electraBuilder) BuildState() (*spec.VersionedBeaconState, error) {
	genesisBlock := b.shadowForkBlock
	if genesisBlock == nil {
		genesisBlock = b.elGenesis.ToBlock()
	}

	genesisBlockHash := genesisBlock.Hash()

	extra := genesisBlock.Extra()
	if len(extra) > 32 {
		return nil, fmt.Errorf("extra data is %d bytes, max is %d", len(extra), 32)
	}

	baseFee, _ := uint256.FromBig(genesisBlock.BaseFee())

	var withdrawalsRoot phase0.Root

	if genesisBlock.Withdrawals() != nil {
		root, err := beaconutils.ComputeWithdrawalsRoot(genesisBlock.Withdrawals(), b.clConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to compute withdrawals root: %w", err)
		}

		withdrawalsRoot = root
	}

	transactionsRoot, err := beaconutils.ComputeTransactionsRoot(genesisBlock.Transactions(), b.clConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compute transactions root: %w", err)
	}

	if genesisBlock.BlobGasUsed() == nil {
		return nil, fmt.Errorf("execution-layer Block has missing blob-gas-used field")
	}

	if genesisBlock.ExcessBlobGas() == nil {
		return nil, fmt.Errorf("execution-layer Block has missing excess-blob-gas field")
	}

	execHeader := &deneb.ExecutionPayloadHeader{
		ParentHash:       phase0.Hash32(genesisBlock.ParentHash()),
		FeeRecipient:     bellatrix.ExecutionAddress(genesisBlock.Coinbase()),
		StateRoot:        phase0.Root(genesisBlock.Root()),
		ReceiptsRoot:     phase0.Root(genesisBlock.ReceiptHash()),
		LogsBloom:        genesisBlock.Bloom(),
		BlockNumber:      genesisBlock.NumberU64(),
		GasLimit:         genesisBlock.GasLimit(),
		GasUsed:          genesisBlock.GasUsed(),
		Timestamp:        genesisBlock.Time(),
		ExtraData:        extra,
		BaseFeePerGas:    baseFee,
		BlockHash:        phase0.Hash32(genesisBlockHash),
		TransactionsRoot: transactionsRoot,
		WithdrawalsRoot:  withdrawalsRoot,
		BlobGasUsed:      *genesisBlock.BlobGasUsed(),
		ExcessBlobGas:    *genesisBlock.ExcessBlobGas(),
	}

	depositRoot, err := beaconutils.ComputeDepositRoot(b.clConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to compute deposit root: %w", err)
	}

	syncCommitteeSize := b.clConfig.GetUintDefault("SYNC_COMMITTEE_SIZE", 512)
	syncCommitteeMaskBytes := syncCommitteeSize / 8

	if syncCommitteeSize%8 != 0 {
		syncCommitteeMaskBytes++
	}

	genesisBlockBody := &electra.BeaconBlockBody{
		ETH1Data: &phase0.ETH1Data{
			BlockHash: make([]byte, 32),
		},
		SyncAggregate: &altair.SyncAggregate{
			SyncCommitteeBits: make([]byte, syncCommitteeMaskBytes),
		},
		ExecutionPayload: &deneb.ExecutionPayload{
			BaseFeePerGas: uint256.NewInt(0),
		},
		ExecutionRequests: &electra.ExecutionRequests{},
	}

	genesisBlockBodyRoot, err := b.dynSsz.HashTreeRoot(genesisBlockBody)
	if err != nil {
		return nil, fmt.Errorf("failed to compute genesis block body root: %w", err)
	}

	clValidators, validatorsRoot := beaconutils.GetGenesisValidators(b.clConfig, b.validators)

	syncCommittee, err := beaconutils.GetGenesisSyncCommittee(b.clConfig, clValidators, phase0.Hash32(genesisBlockHash))
	if err != nil {
		return nil, fmt.Errorf("failed to get genesis sync committee: %w", err)
	}

	genesisDelay := b.clConfig.GetUintDefault("GENESIS_DELAY", 604800)
	blocksPerHistoricalRoot := b.clConfig.GetUintDefault("SLOTS_PER_HISTORICAL_ROOT", 8192)
	epochsPerSlashingVector := b.clConfig.GetUintDefault("EPOCHS_PER_SLASHINGS_VECTOR", 8192)

	minGenesisTime := b.clConfig.GetUintDefault("MIN_GENESIS_TIME", 0)
	if minGenesisTime == 0 {
		minGenesisTime = genesisBlock.Time()
	}

	genesisState := &electra.BeaconState{
		GenesisTime:           minGenesisTime + genesisDelay,
		GenesisValidatorsRoot: validatorsRoot,
		Fork:                  GetStateForkConfig(spec.DataVersionElectra, b.clConfig),
		LatestBlockHeader: &phase0.BeaconBlockHeader{
			BodyRoot: genesisBlockBodyRoot,
		},
		BlockRoots: make([]phase0.Root, blocksPerHistoricalRoot),
		StateRoots: make([]phase0.Root, blocksPerHistoricalRoot),
		ETH1Data: &phase0.ETH1Data{
			DepositRoot: depositRoot,
			BlockHash:   genesisBlockHash[:],
		},
		JustificationBits:            make([]byte, 1),
		PreviousJustifiedCheckpoint:  &phase0.Checkpoint{},
		CurrentJustifiedCheckpoint:   &phase0.Checkpoint{},
		FinalizedCheckpoint:          &phase0.Checkpoint{},
		RANDAOMixes:                  beaconutils.SeedRandomMixes(phase0.Hash32(genesisBlockHash), b.clConfig),
		Validators:                   clValidators,
		Balances:                     beaconutils.GetGenesisBalances(b.clConfig, b.validators),
		Slashings:                    make([]phase0.Gwei, epochsPerSlashingVector),
		PreviousEpochParticipation:   make([]altair.ParticipationFlags, len(clValidators)),
		CurrentEpochParticipation:    make([]altair.ParticipationFlags, len(clValidators)),
		InactivityScores:             make([]uint64, len(clValidators)),
		CurrentSyncCommittee:         syncCommittee,
		NextSyncCommittee:            syncCommittee,
		LatestExecutionPayloadHeader: execHeader,
	}

	beaconutils.ApplyTEEToHeaderFromConfig(genesisState.LatestBlockHeader, b.clConfig)

	// Log header size after TEE fields are applied
	if genesisState.LatestBlockHeader != nil {
		// Try to get SSZ size of header
		headerSSZ, err := b.dynSsz.MarshalSSZ(genesisState.LatestBlockHeader)
		if err == nil {
			logrus.Infof("🔍 GENESIS GENERATOR: BeaconBlockHeader SSZ size after TEE application: %d bytes (expected TEE: 8305, standard: 112)", len(headerSSZ))
			if len(headerSSZ) != 8305 && len(headerSSZ) != 112 {
				logrus.Warnf("⚠️  GENESIS GENERATOR: Unexpected header size %d bytes - neither TEE (8305) nor standard (112)", len(headerSSZ))
			}
		} else {
			logrus.Warnf("Failed to marshal header to SSZ for size check: %v", err)
		}
	}

	versionedState := &spec.VersionedBeaconState{
		Version: spec.DataVersionElectra,
		Electra: genesisState,
	}

	logrus.Infof("genesis version: electra")
	logrus.Infof("genesis time: %v", genesisState.GenesisTime)
	logrus.Infof("genesis validators root: 0x%x", genesisState.GenesisValidatorsRoot)

	return versionedState, nil
}

func (b *electraBuilder) Serialize(state *spec.VersionedBeaconState, contentType http.ContentType) ([]byte, error) {
	if state.Version != spec.DataVersionElectra {
		return nil, fmt.Errorf("unsupported version: %s", state.Version)
	}

	switch contentType {
	case http.ContentTypeSSZ:
		// Log header size before encoding
		if state.Electra != nil && state.Electra.LatestBlockHeader != nil {
			headerSSZ, headerErr := b.dynSsz.MarshalSSZ(state.Electra.LatestBlockHeader)
			if headerErr == nil {
				logrus.Infof("🔍 GENESIS GENERATOR: BeaconBlockHeader SSZ size before state encoding: %d bytes (expected TEE: 8305, standard: 112)", len(headerSSZ))
			}
		}
		
		sszBytes, err := b.dynSsz.MarshalSSZ(state.Electra)
		if err != nil {
			return nil, err
		}
		
		// Analyze the encoded SSZ to check offset values
		if len(sszBytes) > 8369 {
			// Calculate expected fixed portion: genesis_time(8) + genesis_validators_root(32) + slot(8) + fork(16) + header(8305) = 8369
			expectedFixedEnd := 8 + 32 + 8 + 16 + 8305 // 8369
			expectedFixedEndStandard := 8 + 32 + 8 + 16 + 112 // 176
			
			// Read first few offsets
			if len(sszBytes) >= expectedFixedEnd + 4 {
				offset1 := uint32(sszBytes[expectedFixedEnd]) | uint32(sszBytes[expectedFixedEnd+1])<<8 | uint32(sszBytes[expectedFixedEnd+2])<<16 | uint32(sszBytes[expectedFixedEnd+3])<<24
				logrus.Infof("🔍 GENESIS GENERATOR: First offset at position %d: %d (points to byte %d)", expectedFixedEnd, offset1, offset1)
				logrus.Infof("🔍 GENESIS GENERATOR: Expected fixed portion ends at: %d (TEE) or %d (standard)", expectedFixedEnd, expectedFixedEndStandard)
				
				if offset1 < uint32(expectedFixedEnd) {
					logrus.Warnf("⚠️  GENESIS GENERATOR: Offset %d points INTO fixed portion (ends at %d). This suggests dynssz calculated fixed portion assuming %d byte header instead of %d bytes", 
						offset1, expectedFixedEnd, 112, 8305)
				}
			}
		}
		
		logrus.Infof("🔍 GENESIS GENERATOR: Total BeaconState SSZ size: %d bytes", len(sszBytes))
		return sszBytes, nil
	case http.ContentTypeJSON:
		return state.Electra.MarshalJSON()
	default:
		return nil, fmt.Errorf("unsupported content type: %s", contentType)
	}
}
