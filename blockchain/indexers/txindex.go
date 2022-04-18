package indexers

import "github.com/MonteCarloClub/acbc/database"

// TxIndex implements a transaction by hash index.  That is to say, it supports
// querying all transactions by their hash.
type TxIndex struct {
	db         database.DB
	curBlockID uint32
}
