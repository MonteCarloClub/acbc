package wire

import "github.com/MonteCarloClub/acbc/chaincfg/chainhash"

// InvType represents the allowed types of inventory vectors.  See InvVect.
type InvType uint32

// InvVect defines a bitcoin inventory vector which is used to describe data,
// as specified by the Type field, that a peer wants, has, or does not have to
// another peer.
type InvVect struct {
	Type InvType        // Type of data
	Hash chainhash.Hash // Hash of the data
}
