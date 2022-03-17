package mempool

import (
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
	"sync"
)

const (
	// estimateFeeDepth is the maximum number of blocks before a transaction
	// is confirmed that we want to track.
	estimateFeeDepth = 25
)

// SatoshiPerByte is number with units of satoshis per byte.
type SatoshiPerByte float64

// observedTransaction represents an observed transaction and some
// additional data required for the fee estimation algorithm.
type observedTransaction struct {
	// A transaction hash.
	hash chainhash.Hash

	// The fee per byte of the transaction in satoshis.
	feeRate SatoshiPerByte

	// The block height when it was observed.
	observed int32

	// The height of the block in which it was mined.
	// If the transaction has not yet been mined, it is zero.
	mined int32
}

// registeredBlock has the hash of a block and the list of transactions
// it mined which had been previously observed by the FeeEstimator. It
// is used if Rollback is called to reverse the effect of registering
// a block.
type registeredBlock struct {
	hash         chainhash.Hash
	transactions []*observedTransaction
}

// FeeEstimator manages the data necessary to create
// fee estimations. It is safe for concurrent access.
type FeeEstimator struct {
	maxRollback uint32
	binSize     int32

	// The maximum number of replacements that can be made in a single
	// bin per block. Default is estimateFeeMaxReplacements
	maxReplacements int32

	// The minimum number of blocks that can be registered with the fee
	// estimator before it will provide answers.
	minRegisteredBlocks uint32

	// The last known height.
	lastKnownHeight int32

	// The number of blocks that have been registered.
	numBlocksRegistered uint32

	mtx      sync.RWMutex
	observed map[chainhash.Hash]*observedTransaction
	bin      [estimateFeeDepth][]*observedTransaction

	// The cached estimates.
	cached []SatoshiPerByte

	// Transactions that have been removed from the bins. This allows us to
	// revert in case of an orphaned block.
	dropped []*registeredBlock
}
