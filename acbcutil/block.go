package acbcutil

import (
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
	"github.com/MonteCarloClub/acbc/wire"
)

// Block defines a bitcoin block that provides easier and more efficient
// manipulation of raw blocks.  It also memoizes hashes for the block and its
// transactions on their first access so subsequent accesses don't have to
// repeat the relatively expensive hashing operations.
type Block struct {
	msgBlock                 *wire.MsgBlock  // Underlying MsgBlock
	serializedBlock          []byte          // Serialized bytes for the block
	serializedBlockNoWitness []byte          // Serialized bytes for block w/o witness data
	blockHash                *chainhash.Hash // Cached block hash
	blockHeight              int32           // Height in the main block chain
	transactions             []*Tx           // Transactions
	txnsGenerated            bool            // ALL wrapped transactions generated
}
