package main

import (
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MonteCarloClub/acbc/acbcjson"
	"github.com/MonteCarloClub/acbc/acbcutil"
	"github.com/MonteCarloClub/acbc/blockchain"
	"github.com/MonteCarloClub/acbc/blockchain/indexers"
	"github.com/MonteCarloClub/acbc/chaincfg"
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
	"github.com/MonteCarloClub/acbc/database"
	"github.com/MonteCarloClub/acbc/mempool"
	"github.com/MonteCarloClub/acbc/mining"
	"github.com/MonteCarloClub/acbc/mining/cpuminer"
	"github.com/MonteCarloClub/acbc/peer"
	"github.com/MonteCarloClub/acbc/wire"
	"github.com/btcsuite/websocket"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// rpcAuthTimeoutSeconds is the number of seconds a connection to the
	// RPC server is allowed to stay open without authenticating before it
	// is closed.
	rpcAuthTimeoutSeconds = 10

	// uint256Size is the number of bytes needed to represent an unsigned
	// 256-bit integer.
	uint256Size = 32

	// gbtNonceRange is two 32-bit big-endian hexadecimal integers which
	// represent the valid ranges of nonces returned by the getblocktemplate
	// RPC.
	gbtNonceRange = "00000000ffffffff"

	// gbtRegenerateSeconds is the number of seconds that must pass before
	// a new template is generated when the previous block hash has not
	// changed and there have been changes to the available transactions
	// in the memory pool.
	gbtRegenerateSeconds = 60

	// maxProtocolVersion is the max protocol version the server supports.
	maxProtocolVersion = 70002
)

var (

	// JSON 2.0 batched request prefix
	batchedRequestPrefix = []byte("[")
)

// Errors
var (
	// ErrRPCUnimplemented is an error returned to RPC clients when the
	// provided command is recognized, but not implemented.
	ErrRPCUnimplemented = &acbcjson.RPCError{
		Code:    acbcjson.ErrRPCUnimplemented,
		Message: "Command unimplemented",
	}

	// ErrRPCNoWallet is an error returned to RPC clients when the provided
	// command is recognized as a wallet command.
	ErrRPCNoWallet = &acbcjson.RPCError{
		Code:    acbcjson.ErrRPCNoWallet,
		Message: "This implementation does not implement wallet commands",
	}
)

type commandHandler func(*rpcServer, interface{}, <-chan struct{}) (interface{}, error)

// rpcHandlers maps RPC command strings to appropriate handler functions.
// This is set by init because help references rpcHandlers and thus causes
// a dependency loop.
var rpcHandlers map[string]commandHandler
var rpcHandlersBeforeInit = map[string]commandHandler{
	"getblockhash": handleGetBlockHash,
}

// list of commands that we recognize, but for which btcd has no support because
// it lacks support for wallet functionality. For these commands the user
// should ask a connected instance of btcwallet.
var rpcAskWallet = map[string]struct{}{
	"addmultisigaddress":     {},
	"backupwallet":           {},
	"createencryptedwallet":  {},
	"createmultisig":         {},
	"dumpprivkey":            {},
	"dumpwallet":             {},
	"encryptwallet":          {},
	"getaccount":             {},
	"getaccountaddress":      {},
	"getaddressesbyaccount":  {},
	"getbalance":             {},
	"getnewaddress":          {},
	"getrawchangeaddress":    {},
	"getreceivedbyaccount":   {},
	"getreceivedbyaddress":   {},
	"gettransaction":         {},
	"gettxoutsetinfo":        {},
	"getunconfirmedbalance":  {},
	"getwalletinfo":          {},
	"importprivkey":          {},
	"importwallet":           {},
	"keypoolrefill":          {},
	"listaccounts":           {},
	"listaddressgroupings":   {},
	"listlockunspent":        {},
	"listreceivedbyaccount":  {},
	"listreceivedbyaddress":  {},
	"listsinceblock":         {},
	"listtransactions":       {},
	"listunspent":            {},
	"lockunspent":            {},
	"move":                   {},
	"sendfrom":               {},
	"sendmany":               {},
	"sendtoaddress":          {},
	"setaccount":             {},
	"settxfee":               {},
	"signmessage":            {},
	"signrawtransaction":     {},
	"walletlock":             {},
	"walletpassphrase":       {},
	"walletpassphrasechange": {},
}

// Commands that are currently unimplemented, but should ultimately be.
var rpcUnimplemented = map[string]struct{}{
	"estimatepriority": {},
	"getchaintips":     {},
	"getmempoolentry":  {},
	"getnetworkinfo":   {},
	"getwork":          {},
	"invalidateblock":  {},
	"preciousblock":    {},
	"reconsiderblock":  {},
}

// Commands that are available to a limited user
var rpcLimited = map[string]struct{}{
	// Websockets commands
	"loadtxfilter":          {},
	"notifyblocks":          {},
	"notifynewtransactions": {},
	"notifyreceived":        {},
	"notifyspent":           {},
	"rescan":                {},
	"rescanblocks":          {},
	"session":               {},

	// Websockets AND HTTP/S commands
	"help": {},

	// HTTP/S-only commands
	"createrawtransaction":  {},
	"decoderawtransaction":  {},
	"decodescript":          {},
	"estimatefee":           {},
	"getbestblock":          {},
	"getbestblockhash":      {},
	"getblock":              {},
	"getblockcount":         {},
	"getblockhash":          {},
	"getblockheader":        {},
	"getcfilter":            {},
	"getcfilterheader":      {},
	"getcurrentnet":         {},
	"getdifficulty":         {},
	"getheaders":            {},
	"getinfo":               {},
	"getnettotals":          {},
	"getnetworkhashps":      {},
	"getrawmempool":         {},
	"getrawtransaction":     {},
	"gettxout":              {},
	"searchrawtransactions": {},
	"sendrawtransaction":    {},
	"submitblock":           {},
	"uptime":                {},
	"validateaddress":       {},
	"verifymessage":         {},
	"version":               {},
}

// rpcserverPeer represents a peer for use with the RPC server.
//
// The interface contract requires that all of these methods are safe for
// concurrent access.

type rpcserverPeer interface {
	// ToPeer returns the underlying peer instance.
	// 返回实际的peer节点
	ToPeer() *peer.Peer

	// IsTxRelayDisabled returns whether or not the peer has disabled
	// transaction relay.
	// 返回节点是否禁用交易转发
	IsTxRelayDisabled() bool

	// BanScore returns the current integer value that represents how close
	// the peer is to being banned.
	BanScore() uint32

	// FeeFilter returns the requested current minimum fee rate for which
	// transactions should be announced.
	FeeFilter() int64
}

// rpcserverConnManager represents a connection manager for use with the RPC
// server.
//
// The interface contract requires that all of these methods are safe for
// concurrent access.
type rpcserverConnManager interface {
	// Connect adds the provided address as a new outbound peer.  The
	// permanent flag indicates whether or not to make the peer persistent
	// and reconnect if the connection is lost.  Attempting to connect to an
	// already existing peer will return an error.
	Connect(addr string, permanent bool) error

	// RemoveByID removes the peer associated with the provided id from the
	// list of persistent peers.  Attempting to remove an id that does not
	// exist will return an error.
	RemoveByID(id int32) error

	// RemoveByAddr removes the peer associated with the provided address
	// from the list of persistent peers.  Attempting to remove an address
	// that does not exist will return an error.
	RemoveByAddr(addr string) error

	// DisconnectByID disconnects the peer associated with the provided id.
	// This applies to both inbound and outbound peers.  Attempting to
	// remove an id that does not exist will return an error.
	DisconnectByID(id int32) error

	// DisconnectByAddr disconnects the peer associated with the provided
	// address.  This applies to both inbound and outbound peers.
	// Attempting to remove an address that does not exist will return an
	// error.
	DisconnectByAddr(addr string) error

	// ConnectedCount returns the number of currently connected peers.
	ConnectedCount() int32

	// NetTotals returns the sum of all bytes received and sent across the
	// network for all peers.
	NetTotals() (uint64, uint64)

	// ConnectedPeers returns an array consisting of all connected peers.
	ConnectedPeers() []rpcserverPeer

	// PersistentPeers returns an array consisting of all the persistent
	// peers.
	PersistentPeers() []rpcserverPeer

	// BroadcastMessage sends the provided message to all currently
	// connected peers.
	BroadcastMessage(msg wire.Message)

	// AddRebroadcastInventory adds the provided inventory to the list of
	// inventories to be rebroadcast at random intervals until they show up
	// in a block.
	AddRebroadcastInventory(iv *wire.InvVect, data interface{})

	// RelayTransactions generates and relays inventory vectors for all of
	// the passed transactions to all connected peers.
	RelayTransactions(txns []*mempool.TxDesc)

	// NodeAddresses returns an array consisting node addresses which can
	// potentially be used to find new nodes in the network.
	NodeAddresses() []*wire.NetAddress
}

// rpcserverSyncManager represents a sync manager for use with the RPC server.
//
// The interface contract requires that all of these methods are safe for
// concurrent access.
type rpcserverSyncManager interface {
	// IsCurrent returns whether or not the sync manager believes the chain
	// is current as compared to the rest of the network.
	IsCurrent() bool

	// SubmitBlock submits the provided block to the network after
	// processing it locally.
	SubmitBlock(block *acbcutil.Block, flags blockchain.BehaviorFlags) (bool, error)

	// Pause pauses the sync manager until the returned channel is closed.
	Pause() chan<- struct{}

	// SyncPeerID returns the ID of the peer that is currently the peer being
	// used to sync from or 0 if there is none.
	SyncPeerID() int32

	// LocateHeaders returns the headers of the blocks after the first known
	// block in the provided locators until the provided stop hash or the
	// current tip is reached, up to a max of wire.MaxBlockHeadersPerMsg
	// hashes.
	LocateHeaders(locators []*chainhash.Hash, hashStop *chainhash.Hash) []wire.BlockHeader
}

// rpcserverConfig is a descriptor containing the RPC server configuration.
type rpcserverConfig struct {
	// Listeners defines a slice of listeners for which the RPC server will
	// take ownership of and accept connections.  Since the RPC server takes
	// ownership of these listeners, they will be closed when the RPC server
	// is stopped.
	// 侦听器定义了一个侦听器的切片，RPC服务器将对该片段拥有所有权并接受连接。由于RPC服务器拥有
	// 这些侦听器的所有权，因此当RPC服务器停止时，这些侦听器将被关闭。
	Listeners []net.Listener

	// StartupTime is the unix timestamp for when the server that is hosting
	// the RPC server started.
	StartupTime int64

	// ConnMgr defines the connection manager for the RPC server to use.  It
	// provides the RPC server with a means to do things such as add,
	// remove, connect, disconnect, and query peers as well as other
	// connection-related data and tasks.
	ConnMgr rpcserverConnManager

	// SyncMgr defines the sync manager for the RPC server to use.
	SyncMgr rpcserverSyncManager

	// These fields allow the RPC server to interface with the local block
	// chain data and state.
	TimeSource  blockchain.MedianTimeSource
	Chain       *blockchain.BlockChain
	ChainParams *chaincfg.Params
	DB          database.DB

	// TxMemPool defines the transaction memory pool to interact with.
	TxMemPool *mempool.TxPool

	// These fields allow the RPC server to interface with mining.
	//
	// Generator produces block templates and the CPUMiner solves them using
	// the CPU.  CPU mining is typically only useful for test purposes when
	// doing regression or simulation testing.
	Generator *mining.BlkTmplGenerator
	CPUMiner  *cpuminer.CPUMiner

	// These fields define any optional indexes the RPC server can make use
	// of to provide additional data when queried.
	TxIndex   *indexers.TxIndex
	AddrIndex *indexers.AddrIndex
	CfIndex   *indexers.CfIndex

	// The fee estimator keeps track of how long transactions are left in
	// the mempool before they are mined into blocks.
	FeeEstimator *mempool.FeeEstimator
}

// gbtWorkState houses state that is used in between multiple RPC invocations to
// getblocktemplate.
type gbtWorkState struct {
	sync.Mutex
	lastTxUpdate  time.Time
	lastGenerated time.Time
	prevHash      *chainhash.Hash
	minTimestamp  time.Time
	template      *mining.BlockTemplate
	notifyMap     map[chainhash.Hash]map[int64]chan struct{}
	timeSource    blockchain.MedianTimeSource
}

// rpcServer provides a concurrent safe RPC server to a chain server.
type rpcServer struct {
	started                int32
	shutdown               int32
	cfg                    rpcserverConfig
	authsha                [sha256.Size]byte
	limitauthsha           [sha256.Size]byte
	ntfnMgr                *wsNotificationManager
	numClients             int32
	statusLines            map[int]string
	statusLock             sync.RWMutex
	wg                     sync.WaitGroup
	gbtWorkState           *gbtWorkState
	helpCacher             *helpCacher
	requestProcessShutdown chan struct{}
	quit                   chan int
}

// handleUnimplemented is the handler for commands that should ultimately be
// supported but are not yet implemented.
func handleUnimplemented(s *rpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	return nil, ErrRPCUnimplemented
}

// handleAskWallet is the handler for commands that are recognized as valid, but
// are unable to answer correctly since it involves wallet state.
// These commands will be implemented in btcwallet.
func handleAskWallet(s *rpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	return nil, ErrRPCNoWallet
}

// newGbtWorkState returns a new instance of a gbtWorkState with all internal
// fields initialized and ready to use.
func newGbtWorkState(timeSource blockchain.MedianTimeSource) *gbtWorkState {
	return &gbtWorkState{
		notifyMap:  make(map[chainhash.Hash]map[int64]chan struct{}),
		timeSource: timeSource,
	}
}

// internalRPCError is a convenience function to convert an internal error to
// an RPC error with the appropriate code set.  It also logs the error to the
// RPC server subsystem since internal errors really should not occur.  The
// context parameter is only used in the log message and may be empty if it's
// not needed.
func internalRPCError(errStr, context string) *acbcjson.RPCError {
	logStr := errStr
	if context != "" {
		logStr = context + ": " + errStr
	}
	rpcsLog.Error(logStr)
	return acbcjson.NewRPCError(acbcjson.ErrRPCInternal.Code, errStr)
}

// newRPCServer returns a new instance of the rpcServer struct.
func newRPCServer(config *rpcserverConfig) (*rpcServer, error) {
	rpc := rpcServer{
		cfg:                    *config,
		statusLines:            make(map[int]string),
		gbtWorkState:           newGbtWorkState(config.TimeSource),
		helpCacher:             newHelpCacher(),
		requestProcessShutdown: make(chan struct{}),
		quit:                   make(chan int),
	}
	if cfg.RPCUser != "" && cfg.RPCPass != "" {
		login := cfg.RPCUser + ":" + cfg.RPCPass
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(login))
		rpc.authsha = sha256.Sum256([]byte(auth))
	}
	if cfg.RPCLimitUser != "" && cfg.RPCLimitPass != "" {
		login := cfg.RPCLimitUser + ":" + cfg.RPCLimitPass
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(login))
		rpc.limitauthsha = sha256.Sum256([]byte(auth))
	}
	rpc.ntfnMgr = newWsNotificationManager(&rpc)
	//rpc.cfg.Chain.Subscribe(rpc.handleBlockchainNotification)

	return &rpc, nil
}

// handleGetBlockHash implements the getblockhash command.
func handleGetBlockHash(s *rpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	c := cmd.(*acbcjson.GetBlockHashCmd)
	// 这一行测试用
	hash, _ := chainhash.NewHashFromStr(strconv.FormatInt(c.Index, 10))
	/*
		hash, err := s.cfg.Chain.BlockHashByHeight(int32(c.Index))
		if err != nil {
			return nil, &acbcjson.RPCError{
				Code:    acbcjson.ErrRPCOutOfRange,
				Message: "Block number out of range",
			}
		}
	*/
	return hash.String(), nil
}

// limitConnections responds with a 503 service unavailable and returns true if
// adding another client would exceed the maximum allow RPC clients.
//
// This function is safe for concurrent access.
func (s *rpcServer) limitConnections(w http.ResponseWriter, remoteAddr string) bool {
	if int(atomic.LoadInt32(&s.numClients)+1) > cfg.RPCMaxClients {
		rpcsLog.Infof("Max RPC clients exceeded [%d] - "+
			"disconnecting client %s", cfg.RPCMaxClients,
			remoteAddr)
		http.Error(w, "503 Too busy.  Try again later.",
			http.StatusServiceUnavailable)
		return true
	}
	return false
}

// incrementClients adds one to the number of connected RPC clients.  Note
// this only applies to standard clients.  Websocket clients have their own
// limits and are tracked separately.
//
// This function is safe for concurrent access.
func (s *rpcServer) incrementClients() {
	atomic.AddInt32(&s.numClients, 1)
}

// decrementClients subtracts one from the number of connected RPC clients.
// Note this only applies to standard clients.  Websocket clients have their own
// limits and are tracked separately.
//
// This function is safe for concurrent access.
func (s *rpcServer) decrementClients() {
	atomic.AddInt32(&s.numClients, -1)
}

// checkAuth checks the HTTP Basic authentication supplied by a wallet
// or RPC client in the HTTP request r.  If the supplied authentication
// does not match the username and password expected, a non-nil error is
// returned.
//
// This check is time-constant.
//
// The first bool return value signifies auth success (true if successful) and
// the second bool return value specifies whether the user can change the state
// of the server (true) or whether the user is limited (false). The second is
// always false if the first is.
func (s *rpcServer) checkAuth(r *http.Request, require bool) (bool, bool, error) {
	authhdr := r.Header["Authorization"]
	if len(authhdr) <= 0 {
		if require {
			rpcsLog.Warnf("RPC authentication failure from %s",
				r.RemoteAddr)
			return false, false, errors.New("auth failure")
		}

		return false, false, nil
	}

	authsha := sha256.Sum256([]byte(authhdr[0]))

	// Check for limited auth first as in environments with limited users, those
	// are probably expected to have a higher volume of calls
	// re： 当且仅当两个切片 x和y 具有相等的内容时，ConstantTimeCompare 返回1。所花费的时间是切片长度的函数，并且与内容无关。
	limitcmp := subtle.ConstantTimeCompare(authsha[:], s.limitauthsha[:])
	if limitcmp == 1 {
		return true, false, nil
	}

	// Check for admin-level auth
	cmp := subtle.ConstantTimeCompare(authsha[:], s.authsha[:])
	if cmp == 1 {
		return true, true, nil
	}

	// Request's auth doesn't match either user
	rpcsLog.Warnf("RPC authentication failure from %s", r.RemoteAddr)
	return false, false, errors.New("auth failure")
}

// jsonAuthFail sends a message back to the client if the http auth is rejected.
func jsonAuthFail(w http.ResponseWriter) {
	w.Header().Add("WWW-Authenticate", `Basic realm="btcd RPC"`)
	http.Error(w, "401 Unauthorized.", http.StatusUnauthorized)
}

// createMarshalledReply returns a new marshalled JSON-RPC response given the
// passed parameters.  It will automatically convert errors that are not of
// the type *acbcjson.RPCError to the appropriate type as needed.
func createMarshalledReply(rpcVersion acbcjson.RPCVersion, id interface{}, result interface{}, replyErr error) ([]byte, error) {
	var jsonErr *acbcjson.RPCError
	if replyErr != nil {
		if jErr, ok := replyErr.(*acbcjson.RPCError); ok {
			jsonErr = jErr
		} else {
			jsonErr = internalRPCError(replyErr.Error(), "")
		}
	}

	return acbcjson.MarshalResponse(rpcVersion, id, result, jsonErr)
}

// parsedRPCCmd represents a JSON-RPC request object that has been parsed into
// a known concrete command along with any error that might have happened while
// parsing it.
type parsedRPCCmd struct {
	jsonrpc acbcjson.RPCVersion
	id      interface{}
	method  string
	cmd     interface{}
	err     *acbcjson.RPCError
}

// parseCmd parses a JSON-RPC request object into known concrete command.  The
// err field of the returned parsedRPCCmd struct will contain an RPC error that
// is suitable for use in replies if the command is invalid in some way such as
// an unregistered command or invalid parameters.
func parseCmd(request *acbcjson.Request) *parsedRPCCmd {
	parsedCmd := parsedRPCCmd{
		jsonrpc: request.Jsonrpc,
		id:      request.ID,
		method:  request.Method,
	}

	cmd, err := acbcjson.UnmarshalCmd(request)
	if err != nil {
		// When the error is because the method is not registered,
		// produce a method not found RPC error.
		if jerr, ok := err.(acbcjson.Error); ok &&
			jerr.ErrorCode == acbcjson.ErrUnregisteredMethod {

			parsedCmd.err = acbcjson.ErrRPCMethodNotFound
			return &parsedCmd
		}

		// Otherwise, some type of invalid parameters is the
		// cause, so produce the equivalent RPC error.
		parsedCmd.err = acbcjson.NewRPCError(
			acbcjson.ErrRPCInvalidParams.Code, err.Error())
		return &parsedCmd
	}

	parsedCmd.cmd = cmd
	return &parsedCmd
}

// standardCmdResult checks that a parsed command is a standard Bitcoin JSON-RPC
// command and runs the appropriate handler to reply to the command.  Any
// commands which are not recognized or not implemented will return an error
// suitable for use in replies.
func (s *rpcServer) standardCmdResult(cmd *parsedRPCCmd, closeChan <-chan struct{}) (interface{}, error) {
	handler, ok := rpcHandlers[cmd.method]
	if ok {
		goto handled
	}
	_, ok = rpcAskWallet[cmd.method]
	if ok {
		handler = handleAskWallet
		goto handled
	}
	_, ok = rpcUnimplemented[cmd.method]
	if ok {
		handler = handleUnimplemented
		goto handled
	}
	return nil, acbcjson.ErrRPCMethodNotFound
handled:

	return handler(s, cmd.cmd, closeChan)
}

// processRequest determines the incoming request type (single or batched),
// parses it and returns a marshalled response.
func (s *rpcServer) processRequest(request *acbcjson.Request, isAdmin bool, closeChan <-chan struct{}) []byte {
	var result interface{}
	var err error
	var jsonErr *acbcjson.RPCError

	if !isAdmin {
		if _, ok := rpcLimited[request.Method]; !ok {
			jsonErr = internalRPCError("limited user not "+
				"authorized for this method", "")
		}
	}

	if jsonErr == nil {
		if request.Method == "" || request.Params == nil {
			jsonErr = &acbcjson.RPCError{
				Code:    acbcjson.ErrRPCInvalidRequest.Code,
				Message: "Invalid request: malformed",
			}
			msg, err := createMarshalledReply(request.Jsonrpc, request.ID, result, jsonErr)
			if err != nil {
				rpcsLog.Errorf("Failed to marshal reply: %v", err)
				return nil
			}
			return msg
		}

		// Valid requests with no ID (notifications) must not have a response
		// per the JSON-RPC spec.
		// re：根据规范，没有id的有效请求，也即通知，必须不能有响应
		if request.ID == nil {
			return nil
		}

		// Attempt to parse the JSON-RPC request into a known
		// concrete command.
		parsedCmd := parseCmd(request)
		if parsedCmd.err != nil {
			jsonErr = parsedCmd.err
		} else {
			result, err = s.standardCmdResult(parsedCmd,
				closeChan)
			if err != nil {
				if rpcErr, ok := err.(*acbcjson.RPCError); ok {
					jsonErr = rpcErr
				} else {
					jsonErr = &acbcjson.RPCError{
						Code:    acbcjson.ErrRPCInvalidRequest.Code,
						Message: "Invalid request: malformed",
					}
				}
			}
		}
	}

	// Marshal the response.
	msg, err := createMarshalledReply(request.Jsonrpc, request.ID, result, jsonErr)
	if err != nil {
		rpcsLog.Errorf("Failed to marshal reply: %v", err)
		return nil
	}
	return msg
}

// httpStatusLine returns a response Status-Line (RFC 2616 Section 6.1)
// for the given request and response status code.  This function was lifted and
// adapted from the standard library HTTP server code since it's not exported.
func (s *rpcServer) httpStatusLine(req *http.Request, code int) string {
	// Fast path:
	key := code
	proto11 := req.ProtoAtLeast(1, 1)
	if !proto11 {
		key = -key
	}
	s.statusLock.RLock()
	line, ok := s.statusLines[key]
	s.statusLock.RUnlock()
	if ok {
		return line
	}

	// Slow path:
	proto := "HTTP/1.0"
	if proto11 {
		proto = "HTTP/1.1"
	}
	codeStr := strconv.Itoa(code)
	text := http.StatusText(code)
	if text != "" {
		line = proto + " " + codeStr + " " + text + "\r\n"
		s.statusLock.Lock()
		s.statusLines[key] = line
		s.statusLock.Unlock()
	} else {
		text = "status code " + codeStr
		line = proto + " " + codeStr + " " + text + "\r\n"
	}

	return line
}

// writeHTTPResponseHeaders writes the necessary response headers prior to
// writing an HTTP body given a request to use for protocol negotiation, headers
// to write, a status code, and a writer.
func (s *rpcServer) writeHTTPResponseHeaders(req *http.Request, headers http.Header, code int, w io.Writer) error {
	_, err := io.WriteString(w, s.httpStatusLine(req, code))
	if err != nil {
		return err
	}

	err = headers.Write(w)
	if err != nil {
		return err
	}

	_, err = io.WriteString(w, "\r\n")
	return err
}

// jsonRPCRead handles reading and responding to RPC messages.
func (s *rpcServer) jsonRPCRead(w http.ResponseWriter, r *http.Request, isAdmin bool) {
	if atomic.LoadInt32(&s.shutdown) != 0 {
		return
	}

	// Read and close the JSON-RPC request body from the caller.
	body, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		errCode := http.StatusBadRequest
		http.Error(w, fmt.Sprintf("%d error reading JSON message: %v",
			errCode, err), errCode)
		return
	}

	// Unfortunately, the http server doesn't provide the ability to
	// change the read deadline for the new connection and having one breaks
	// long polling.  However, not having a read deadline on the initial
	// connection would mean clients can connect and idle forever.  Thus,
	// hijack the connecton from the HTTP server, clear the read deadline,
	// and handle writing the response manually.
	// re：http 服务器不提供更改新连接的读取期限和中断长轮询的能力。但是，初始连接没有读取截止日期意味
	//   着客户端可以连接并永远空闲。因此，从 HTTP 服务器劫持连接，清除读取期限，并手动处理写入响应。
	// q：？？
	hj, ok := w.(http.Hijacker)
	if !ok {
		errMsg := "webserver doesn't support hijacking"
		rpcsLog.Warnf(errMsg)
		errCode := http.StatusInternalServerError
		http.Error(w, strconv.Itoa(errCode)+" "+errMsg, errCode)
		return
	}
	conn, buf, err := hj.Hijack()
	if err != nil {
		rpcsLog.Warnf("Failed to hijack HTTP connection: %v", err)
		errCode := http.StatusInternalServerError
		http.Error(w, strconv.Itoa(errCode)+" "+err.Error(), errCode)
		return
	}
	defer conn.Close()
	defer buf.Flush()
	conn.SetReadDeadline(timeZeroVal)

	// Attempt to parse the raw body into a JSON-RPC request.
	// Setup a close notifier.  Since the connection is hijacked,
	// the CloseNotifer on the ResponseWriter is not available.
	// q： ？？？
	closeChan := make(chan struct{}, 1)
	go func() {
		_, err = conn.Read(make([]byte, 1))
		if err != nil {
			close(closeChan)
		}
	}()

	var results []json.RawMessage
	var batchSize int
	var batchedRequest bool

	// Determine request type
	if bytes.HasPrefix(body, batchedRequestPrefix) {
		batchedRequest = true
	}

	// Process a single request
	if !batchedRequest {
		var req acbcjson.Request
		var resp json.RawMessage
		err = json.Unmarshal(body, &req)
		if err != nil {
			jsonErr := &acbcjson.RPCError{
				Code: acbcjson.ErrRPCParse.Code,
				Message: fmt.Sprintf("Failed to parse request: %v",
					err),
			}
			resp, err = acbcjson.MarshalResponse(acbcjson.RpcVersion1, nil, nil, jsonErr)
			if err != nil {
				rpcsLog.Errorf("Failed to create reply: %v", err)
			}
		}

		if err == nil {
			// The JSON-RPC 1.0 spec defines that notifications must have their "id"
			// set to null and states that notifications do not have a response.
			// re： 1.0中 通知必须把 id 置为 null，并且通知不会有响应
			// A JSON-RPC 2.0 notification is a request with "json-rpc":"2.0", and
			// without an "id" member. The specification states that notifications
			// must not be responded to. JSON-RPC 2.0 permits the null value as a
			// valid request id, therefore such requests are not notifications.
			// re： 2.0中 通知是一个没有 id成员的 rpc 2.0的request，并且通知一定也不会响应。
			//   2.0 许可null值作为有效的请求id，因此id为null的request不是通知。
			// Bitcoin Core serves requests with "id":null or even an absent "id",
			// and responds to such requests with "id":null in the response.
			// re：比特币内核对请求 id为null或者缺失id的都会服务，会对id为null的请求进行响应
			// Btcd does not respond to any request without and "id" or "id":null,
			// regardless the indicated JSON-RPC protocol version unless RPC quirks
			// are enabled. With RPC quirks enabled, such requests will be responded
			// to if the reqeust does not indicate JSON-RPC version.
			// re：btcd 则不会响应任何 id为null或者缺失id的 request，除非 rpc quirks被使用。
			//  当使用rpc quirks时，如果没有描述版本，那么这类request就会被响应
			// RPC quirks can be enabled by the user to avoid compatibility issues
			// with software relying on Core's behavior.
			// re： rpc quirks 可以被用户使能，来避免与依赖Core行为的软件的兼容性问题
			if req.ID == nil && !(cfg.RPCQuirks && req.Jsonrpc == "") {
				return
			}
			resp = s.processRequest(&req, isAdmin, closeChan)
		}

		if resp != nil {
			results = append(results, resp)
		}
	}

	// Process a batched request
	if batchedRequest {
		var batchedRequests []interface{}
		var resp json.RawMessage
		err = json.Unmarshal(body, &batchedRequests)
		if err != nil {
			jsonErr := &acbcjson.RPCError{
				Code: acbcjson.ErrRPCParse.Code,
				Message: fmt.Sprintf("Failed to parse request: %v",
					err),
			}
			resp, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
			if err != nil {
				rpcsLog.Errorf("Failed to create reply: %v", err)
			}

			if resp != nil {
				results = append(results, resp)
			}
		}

		if err == nil {
			// Response with an empty batch error if the batch size is zero
			if len(batchedRequests) == 0 {
				jsonErr := &acbcjson.RPCError{
					Code:    acbcjson.ErrRPCInvalidRequest.Code,
					Message: "Invalid request: empty batch",
				}
				resp, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal reply: %v", err)
				}

				if resp != nil {
					results = append(results, resp)
				}
			}

			// Process each batch entry individually
			if len(batchedRequests) > 0 {
				batchSize = len(batchedRequests)

				for _, entry := range batchedRequests {
					var reqBytes []byte
					reqBytes, err = json.Marshal(entry)
					if err != nil {
						jsonErr := &acbcjson.RPCError{
							Code: acbcjson.ErrRPCInvalidRequest.Code,
							Message: fmt.Sprintf("Invalid request: %v",
								err),
						}
						resp, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
						if err != nil {
							rpcsLog.Errorf("Failed to create reply: %v", err)
						}

						if resp != nil {
							results = append(results, resp)
						}
						continue
					}

					var req acbcjson.Request
					err := json.Unmarshal(reqBytes, &req)
					if err != nil {
						jsonErr := &acbcjson.RPCError{
							Code: acbcjson.ErrRPCInvalidRequest.Code,
							Message: fmt.Sprintf("Invalid request: %v",
								err),
						}
						resp, err = acbcjson.MarshalResponse("", nil, nil, jsonErr)
						if err != nil {
							rpcsLog.Errorf("Failed to create reply: %v", err)
						}

						if resp != nil {
							results = append(results, resp)
						}
						continue
					}

					resp = s.processRequest(&req, isAdmin, closeChan)
					if resp != nil {
						results = append(results, resp)
					}
				}
			}
		}
	}

	var msg = []byte{}
	if batchedRequest && batchSize > 0 {
		if len(results) > 0 {
			// Form the batched response json
			var buffer bytes.Buffer
			buffer.WriteByte('[')
			for idx, reply := range results {
				if idx == len(results)-1 {
					buffer.Write(reply)
					buffer.WriteByte(']')
					break
				}
				buffer.Write(reply)
				buffer.WriteByte(',')
			}
			msg = buffer.Bytes()
		}
	}

	if !batchedRequest || batchSize == 0 {
		// Respond with the first results entry for single requests
		if len(results) > 0 {
			msg = results[0]
		}
	}

	// Write the response.
	err = s.writeHTTPResponseHeaders(r, w.Header(), http.StatusOK, buf)
	if err != nil {
		rpcsLog.Error(err)
		return
	}
	if _, err := buf.Write(msg); err != nil {
		rpcsLog.Errorf("Failed to write marshalled reply: %v", err)
	}

	// Terminate with newline to maintain compatibility with Bitcoin Core.
	// re: 用换行符终止以保持与比特币核心的兼容性
	if err := buf.WriteByte('\n'); err != nil {
		rpcsLog.Errorf("Failed to append terminating newline to reply: %v", err)
	}
}

// Start is used by server.go to start the rpc listener.
func (s *rpcServer) Start() {
	if atomic.AddInt32(&s.started, 1) != 1 {
		return
	}
	rpcsLog.Trace("Starting RPC server")
	rpcServeMux := http.NewServeMux()
	httpServer := &http.Server{
		Handler: rpcServeMux,

		// Timeout connections which don't complete the initial
		// handshake within the allowed timeframe.
		ReadTimeout: time.Second * rpcAuthTimeoutSeconds,
		// re：忽略的字段为0或者空
	}
	rpcServeMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Connection", "close") // re：不支持长连接
		w.Header().Set("Content-Type", "application/json")
		r.Close = true // q：？？

		// Limit the number of connections to max allowed.
		if s.limitConnections(w, r.RemoteAddr) {
			return
		}

		// Keep track of the number of connected clients.
		s.incrementClients()
		defer s.decrementClients()
		_, isAdmin, err := s.checkAuth(r, true)
		if err != nil {
			jsonAuthFail(w)
			return
		}

		// Read and respond to the request.
		s.jsonRPCRead(w, r, isAdmin)
	})

	// Websocket endpoint.
	rpcServeMux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		authenticated, isAdmin, err := s.checkAuth(r, false)
		if err != nil {
			jsonAuthFail(w)
			return
		}

		// Attempt to upgrade the connection to a websocket connection
		// using the default size for read/write buffers.
		ws, err := websocket.Upgrade(w, r, nil, 0, 0)
		if err != nil {
			if _, ok := err.(websocket.HandshakeError); !ok {
				rpcsLog.Errorf("Unexpected websocket error: %v",
					err)
			}
			http.Error(w, "400 Bad Request.", http.StatusBadRequest)
			return
		}
		s.WebsocketHandler(ws, r.RemoteAddr, authenticated, isAdmin)
	})
	for _, listener := range s.cfg.Listeners {

		s.wg.Add(1)
		go func(listener net.Listener) {
			rpcsLog.Infof("RPC server listening on %s", listener.Addr())
			httpServer.Serve(listener)
			rpcsLog.Tracef("RPC listener done for %s", listener.Addr())
			s.wg.Done()
		}(listener)
	}
	//time.Sleep(10 * time.Second)
	s.ntfnMgr.Start()
}

// genCertPair generates a key/cert pair to the paths provided.
func genCertPair(certFile, keyFile string) error {
	rpcsLog.Infof("Generating TLS certificates...")

	org := "btcd autogenerated cert"
	validUntil := time.Now().Add(10 * 365 * 24 * time.Hour)
	cert, key, err := acbcutil.NewTLSCertPair(org, validUntil, nil)
	if err != nil {
		return err
	}

	// Write cert and key files.
	if err = ioutil.WriteFile(certFile, cert, 0666); err != nil {
		return err
	}
	if err = ioutil.WriteFile(keyFile, key, 0600); err != nil {
		os.Remove(certFile)
		return err
	}

	rpcsLog.Infof("Done generating TLS certificates")
	return nil
}

// RequestedProcessShutdown returns a channel that is sent to when an authorized
// RPC client requests the process to shutdown.  If the request can not be read
// immediately, it is dropped.
func (s *rpcServer) RequestedProcessShutdown() <-chan struct{} {
	return s.requestProcessShutdown
}

// Shutdown shuts down the manager, stopping the notification queue and
// notification handler goroutines.
func (m *wsNotificationManager) Shutdown() {
	close(m.quit)
}

// Stop is used by server.go to stop the rpc listener.
func (s *rpcServer) Stop() error {
	if atomic.AddInt32(&s.shutdown, 1) != 1 {
		rpcsLog.Infof("RPC server is already in the process of shutting down")
		return nil
	}
	rpcsLog.Warnf("RPC server shutting down")
	for _, listener := range s.cfg.Listeners {
		err := listener.Close()
		if err != nil {
			rpcsLog.Errorf("Problem shutting down rpc: %v", err)
			return err
		}
	}
	s.ntfnMgr.Shutdown()
	s.ntfnMgr.WaitForShutdown()
	close(s.quit)
	s.wg.Wait()
	rpcsLog.Infof("RPC server shutdown complete")
	return nil
}

func init() {
	rpcHandlers = rpcHandlersBeforeInit
	rand.Seed(time.Now().UnixNano())
}
