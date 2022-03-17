package indexers

import (
	"github.com/MonteCarloClub/acbc/chaincfg"
)

// CfIndex implements a committed filter (cf) by hash index.
type CfIndex struct {
	//db          database.DB
	chainParams *chaincfg.Params
}
