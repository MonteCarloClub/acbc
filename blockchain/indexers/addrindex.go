package indexers

import (
	"github.com/MonteCarloClub/acbc/chaincfg"
	"sync"
)

// AddrIndex implements a transaction by address index.  That is to say, it
// supports querying all transactions that reference a given address because
// they are either crediting or debiting the address.  The returned transactions
// are ordered according to their order of appearance in the blockchain.  In
// other words, first by block height and then by offset inside the block.
//
// In addition, support is provided for a memory-only index of unconfirmed
// transactions such as those which are kept in the memory pool before inclusion
// in a block.
type AddrIndex struct {
	// The following fields are set when the instance is created and can't
	// be changed afterwards, so there is no need to protect them with a
	// separate mutex.
	//db          database.DB
	chainParams *chaincfg.Params

	// The following fields are used to quickly link transactions and
	// addresses that have not been included into a block yet when an
	// address index is being maintained.  The are protected by the
	// unconfirmedLock field.
	//
	// The txnsByAddr field is used to keep an index of all transactions
	// which either create an output to a given address or spend from a
	// previous output to it keyed by the address.
	//
	// The addrsByTx field is essentially the reverse and is used to
	// keep an index of all addresses which a given transaction involves.
	// This allows fairly efficient updates when transactions are removed
	// once they are included into a block.
	unconfirmedLock sync.RWMutex
	//txnsByAddr      map[[addrKeySize]byte]map[chainhash.Hash]*btcutil.Tx
	//addrsByTx       map[chainhash.Hash]map[[addrKeySize]byte]struct{}
}
