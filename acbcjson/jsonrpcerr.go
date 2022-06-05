package acbcjson

// Specific Errors related to commands.  These are the ones a user of the RPC
// server are most likely to see.  Generally, the codes should match one of the
// more general errors above.

// Standard JSON-RPC 2.0 errors.
var (
	ErrRPCInvalidRequest = &RPCError{
		Code:    -32600,
		Message: "Invalid request",
	}
	ErrRPCMethodNotFound = &RPCError{
		Code:    -32601,
		Message: "Method not found",
	}
	ErrRPCInvalidParams = &RPCError{
		Code:    -32602,
		Message: "Invalid parameters",
	}
	ErrRPCInternal = &RPCError{
		Code:    -32603,
		Message: "Internal error",
	}
	ErrRPCParse = &RPCError{
		Code:    -32700,
		Message: "Parse error",
	}
)

// Errors that are specific to acbc.
const (
	ErrRPCNoWallet      RPCErrorCode = -1
	ErrRPCUnimplemented RPCErrorCode = -1
)
