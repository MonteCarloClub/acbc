package rpcserver

import (
	"crypto/sha256"
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
	"net"
	"sync"
	"time"
)

// rpcserverPeer represents a peer for use with the RPC server.
//
// The interface contract requires that all of these methods are safe for
// concurrent access.
type rpcserverPeer interface {
	// ToPeer returns the underlying peer instance.
	ToPeer() *peer.Peer

	// IsTxRelayDisabled returns whether or not the peer has disabled
	// transaction relay.
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

type commandHandler func(*rpcServer, interface{}, <-chan struct{}) (interface{}, error)

// rpcHandlers maps RPC command strings to appropriate handler functions.
// This is set by init because help references rpcHandlers and thus causes
// a dependency loop.
var rpcHandlers map[string]commandHandler
var rpcHandlersBeforeInit = map[string]commandHandler{
	"getblockhash": handleGetBlockHash,
}

// handleGetBlockHash implements the getblockhash command.
func handleGetBlockHash(s *rpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	c := cmd.(*acbcjson.GetBlockHashCmd)
	hash, err := s.cfg.Chain.BlockHashByHeight(int32(c.Index))
	if err != nil {
		return nil, &acbcjson.RPCError{
			Code:    acbcjson.ErrRPCOutOfRange,
			Message: "Block number out of range",
		}
	}

	return hash.String(), nil
}
