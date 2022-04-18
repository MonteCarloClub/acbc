package mempool

import (
	"github.com/MonteCarloClub/acbc/mining"
	"time"
)

const (

	// DefaultBlockPrioritySize is the default size in bytes for high-
	// priority / low-fee transactions.  It is used to help determine which
	// are allowed into the mempool and consequently affects their relay and
	// inclusion when generating block templates.
	DefaultBlockPrioritySize = 50000
)

// TxDesc is a descriptor containing a transaction in the mempool along with
// additional metadata.
type TxDesc struct {
	mining.TxDesc

	// StartingPriority is the priority of the transaction when it was added
	// to the pool.
	StartingPriority float64
}

// TxPool is used as a source of transactions that need to be mined into blocks
// and relayed to other peers.  It is safe for concurrent access from multiple
// peers.
type TxPool struct {
	// The following variables must only be used atomically.
	lastUpdated int64 // last time pool was updated
	/*
		mtx           sync.RWMutex
		cfg           Config
		pool          map[chainhash.Hash]*TxDesc
		orphans       map[chainhash.Hash]*orphanTx
		orphansByPrev map[wire.OutPoint]map[chainhash.Hash]*btcutil.Tx
		outpoints     map[wire.OutPoint]*btcutil.Tx
		pennyTotal    float64 // exponentially decaying total for penny spends.
		lastPennyUnix int64   // unix time of last ``penny spend''
	*/
	// nextExpireScan is the time after which the orphan pool will be
	// scanned in order to evict orphans.  This is NOT a hard deadline as
	// the scan will only run when an orphan is added to the pool as opposed
	// to on an unconditional timer.
	nextExpireScan time.Time
}
