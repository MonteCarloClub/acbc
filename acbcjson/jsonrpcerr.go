package acbcjson

// Specific Errors related to commands.  These are the ones a user of the RPC
// server are most likely to see.  Generally, the codes should match one of the
// more general errors above.

const (
	ErrRPCOutOfRange RPCErrorCode = -1
)
