package blockchain

import (
	"fmt"
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
)

// BlockChain provides functions for working with the bitcoin block chain.
// It includes functionality such as rejecting duplicate blocks, ensuring blocks
// follow all rules, orphan handling, checkpoint handling, and best chain
// selection with reorganization.
type BlockChain struct {
	/*
		// The following fields are set when the instance is created and can't
		// be changed afterwards, so there is no need to protect them with a
		// separate mutex.
		checkpoints         []chaincfg.Checkpoint
		checkpointsByHeight map[int32]*chaincfg.Checkpoint
		db                  database.DB
		chainParams         *chaincfg.Params
		timeSource          MedianTimeSource
		sigCache            *txscript.SigCache
		indexManager        IndexManager
		hashCache           *txscript.HashCache

		// The following fields are calculated based upon the provided chain
		// parameters.  They are also set when the instance is created and
		// can't be changed afterwards, so there is no need to protect them with
		// a separate mutex.
		minRetargetTimespan int64 // target timespan / adjustment factor
		maxRetargetTimespan int64 // target timespan * adjustment factor
		blocksPerRetarget   int32 // target timespan / target time per block

		// chainLock protects concurrent access to the vast majority of the
		// fields in this struct below this point.
		chainLock sync.RWMutex

		// These fields are related to the memory block index.  They both have
		// their own locks, however they are often also protected by the chain
		// lock to help prevent logic races when blocks are being processed.
		//
		// index houses the entire block index in memory.  The block index is
		// a tree-shaped structure.
		//
		// bestChain tracks the current active chain by making use of an
		// efficient chain view into the block index.
		index     *blockIndex
	*/

	bestChain *chainView

	/*
		// These fields are related to handling of orphan blocks.  They are
		// protected by a combination of the chain lock and the orphan lock.
		orphanLock   sync.RWMutex
		orphans      map[chainhash.Hash]*orphanBlock
		prevOrphans  map[chainhash.Hash][]*orphanBlock
		oldestOrphan *orphanBlock

		// These fields are related to checkpoint handling.  They are protected
		// by the chain lock.
		nextCheckpoint *chaincfg.Checkpoint
		checkpointNode *blockNode

		// The state is used as a fairly efficient way to cache information
		// about the current best chain state that is returned to callers when
		// requested.  It operates on the principle of MVCC such that any time a
		// new block becomes the best block, the state pointer is replaced with
		// a new struct and the old state is left untouched.  In this way,
		// multiple callers can be pointing to different best chain states.
		// This is acceptable for most callers because the state is only being
		// queried at a specific point in time.
		//
		// In addition, some of the fields are stored in the database so the
		// chain state can be quickly reconstructed on load.
		stateLock     sync.RWMutex
		stateSnapshot *BestState

		// The following caches are used to efficiently keep track of the
		// current deployment threshold state of each rule change deployment.
		//
		// This information is stored in the database so it can be quickly
		// reconstructed on load.
		//
		// warningCaches caches the current deployment threshold state for blocks
		// in each of the **possible** deployments.  This is used in order to
		// detect when new unrecognized rule changes are being voted on and/or
		// have been activated such as will be the case when older versions of
		// the software are being used
		//
		// deploymentCaches caches the current deployment threshold state for
		// blocks in each of the actively defined deployments.
		warningCaches    []thresholdStateCache
		deploymentCaches []thresholdStateCache

		// The following fields are used to determine if certain warnings have
		// already been shown.
		//
		// unknownRulesWarned refers to warnings due to unknown rules being
		// activated.
		unknownRulesWarned bool

		// The notifications field stores a slice of callbacks to be executed on
		// certain blockchain events.
		notificationsLock sync.RWMutex
		notifications     []NotificationCallback

	*/
}

// BlockHashByHeight returns the hash of the block at the given height in the
// main chain.
//
// This function is safe for concurrent access.
func (b *BlockChain) BlockHashByHeight(blockHeight int32) (*chainhash.Hash, error) {
	node := b.bestChain.NodeByHeight(blockHeight)
	if node == nil {
		str := fmt.Sprintf("no block at height %d exists", blockHeight)
		return nil, errNotInMainChain(str)

	}

	return &node.hash, nil
}
