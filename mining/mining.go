package mining

import (
	"github.com/MonteCarloClub/acbc/acbcutil"
	"github.com/MonteCarloClub/acbc/wire"
	"time"
)

// TxDesc is a descriptor about a transaction in a transaction source along with
// additional metadata.
type TxDesc struct {
	// Tx is the transaction associated with the entry.
	Tx *acbcutil.Tx

	// Added is the time when the entry was added to the source pool.
	Added time.Time

	// Height is the block height when the entry was added to the the source
	// pool.
	Height int32

	// Fee is the total fee the transaction associated with the entry pays.
	Fee int64

	// FeePerKB is the fee the transaction pays in Satoshi per 1000 bytes.
	FeePerKB int64
}

// BlockTemplate houses a block that has yet to be solved along with additional
// details about the fees and the number of signature operations for each
// transaction in the block.
type BlockTemplate struct {
	// Block is a block that is ready to be solved by miners.  Thus, it is
	// completely valid with the exception of satisfying the proof-of-work
	// requirement.
	Block *wire.MsgBlock

	// Fees contains the amount of fees each transaction in the generated
	// template pays in base units.  Since the first transaction is the
	// coinbase, the first entry (offset 0) will contain the negative of the
	// sum of the fees of all other transactions.
	Fees []int64

	// SigOpCosts contains the number of signature operations each
	// transaction in the generated template performs.
	SigOpCosts []int64

	// Height is the height at which the block template connects to the main
	// chain.
	Height int32

	// ValidPayAddress indicates whether or not the template coinbase pays
	// to an address or is redeemable by anyone.  See the documentation on
	// NewBlockTemplate for details on which this can be useful to generate
	// templates without a coinbase payment address.
	ValidPayAddress bool

	// WitnessCommitment is a commitment to the witness data (if any)
	// within the block. This field will only be populted once segregated
	// witness has been activated, and the block contains a transaction
	// which has witness data.
	WitnessCommitment []byte
}

// BlkTmplGenerator provides a type that can be used to generate block templates
// based on a given mining policy and source of transactions to choose from.
// It also houses additional state required in order to ensure the templates
// are built on top of the current best chain and adhere to the consensus rules.
type BlkTmplGenerator struct {
	/*
		policy      *Policy
		chainParams *chaincfg.Params
		txSource    TxSource
		chain       *blockchain.BlockChain
		timeSource  blockchain.MedianTimeSource
		sigCache    *txscript.SigCache
		hashCache   *txscript.HashCache

	*/
}
