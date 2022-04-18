package acbcjson

// LoadTxFilterCmd defines the loadtxfilter request parameters to load or
// reload a transaction filter.
//
// NOTE: This is a btcd extension ported from github.com/decred/dcrd/dcrjson
// and requires a websocket connection.
type LoadTxFilterCmd struct {
	Reload    bool
	Addresses []string
	OutPoints []OutPoint
}

// OutPoint describes a transaction outpoint that will be marshalled to and
// from JSON.
type OutPoint struct {
	Hash  string `json:"hash"`
	Index uint32 `json:"index"`
}

// AuthenticateCmd defines the authenticate JSON-RPC command.
type AuthenticateCmd struct {
	Username   string
	Passphrase string
}

// RescanCmd defines the rescan JSON-RPC command.
//
// Deprecated: Use RescanBlocksCmd instead.
type RescanCmd struct {
	BeginBlock string
	Addresses  []string
	OutPoints  []OutPoint
	EndBlock   *string
}

// NewLoadTxFilterCmd returns a new instance which can be used to issue a
// loadtxfilter JSON-RPC command.
//
// NOTE: This is a btcd extension ported from github.com/decred/dcrd/dcrjson
// and requires a websocket connection.
func NewLoadTxFilterCmd(reload bool, addresses []string, outPoints []OutPoint) *LoadTxFilterCmd {
	return &LoadTxFilterCmd{
		Reload:    reload,
		Addresses: addresses,
		OutPoints: outPoints,
	}
}

// NewRescanCmd returns a new instance which can be used to issue a rescan
// JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
//
// Deprecated: Use NewRescanBlocksCmd instead.
func NewRescanCmd(beginBlock string, addresses []string, outPoints []OutPoint, endBlock *string) *RescanCmd {
	return &RescanCmd{
		BeginBlock: beginBlock,
		Addresses:  addresses,
		OutPoints:  outPoints,
		EndBlock:   endBlock,
	}
}

// NotifyReceivedCmd defines the notifyreceived JSON-RPC command.
//
// Deprecated: Use LoadTxFilterCmd instead.
type NotifyReceivedCmd struct {
	Addresses []string
}

// NewNotifyReceivedCmd returns a new instance which can be used to issue a
// notifyreceived JSON-RPC command.
//
// Deprecated: Use NewLoadTxFilterCmd instead.
func NewNotifyReceivedCmd(addresses []string) *NotifyReceivedCmd {
	return &NotifyReceivedCmd{
		Addresses: addresses,
	}
}

// NotifyNewTransactionsCmd defines the notifynewtransactions JSON-RPC command.
type NotifyNewTransactionsCmd struct {
	Verbose *bool `jsonrpcdefault:"false"`
}

// NewNotifyNewTransactionsCmd returns a new instance which can be used to issue
// a notifynewtransactions JSON-RPC command.
//
// The parameters which are pointers indicate they are optional.  Passing nil
// for optional parameters will use the default value.
func NewNotifyNewTransactionsCmd(verbose *bool) *NotifyNewTransactionsCmd {
	return &NotifyNewTransactionsCmd{
		Verbose: verbose,
	}
}

// NotifySpentCmd defines the notifyspent JSON-RPC command.
//
// Deprecated: Use LoadTxFilterCmd instead.
type NotifySpentCmd struct {
	OutPoints []OutPoint
}

// NewNotifySpentCmd returns a new instance which can be used to issue a
// notifyspent JSON-RPC command.
//
// Deprecated: Use NewLoadTxFilterCmd instead.
func NewNotifySpentCmd(outPoints []OutPoint) *NotifySpentCmd {
	return &NotifySpentCmd{
		OutPoints: outPoints,
	}
}

// NotifyBlocksCmd defines the notifyblocks JSON-RPC command.
type NotifyBlocksCmd struct{}

// NewNotifyBlocksCmd returns a new instance which can be used to issue a
// notifyblocks JSON-RPC command.
func NewNotifyBlocksCmd() *NotifyBlocksCmd {
	return &NotifyBlocksCmd{}
}
