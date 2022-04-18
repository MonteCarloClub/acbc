package mempool

import "github.com/MonteCarloClub/acbc/acbcutil"

const (
	// DefaultMinRelayTxFee is the minimum fee in satoshi that is required
	// for a transaction to be treated as free for relay and mining
	// purposes.  It is also used to help determine if a transaction is
	// considered dust and as a base for calculating minimum required fees
	// for larger transactions.  This value is in Satoshi/1000 bytes.
	DefaultMinRelayTxFee = acbcutil.Amount(1000)
)
