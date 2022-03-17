package peer

import (
	"github.com/MonteCarloClub/acbc/chaincfg"
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
	"github.com/MonteCarloClub/acbc/wire"
	"github.com/decred/dcrd/lru"
	"net"
	"sync"
	"time"
)

// stallControlCmd represents the command of a stall control message.
type stallControlCmd uint8

// stallControlMsg is used to signal the stall handler about specific events
// so it can properly detect and handle stalled remote peers.
type stallControlMsg struct {
	command stallControlCmd
	message wire.Message
}

// HashFunc is a function which returns a block hash, height and error
// It is used as a callback to get newest block details.
type HashFunc func() (hash *chainhash.Hash, height int32, err error)

// HostToNetAddrFunc is a func which takes a host, port, services and returns
// the netaddress.
type HostToNetAddrFunc func(host string, port uint16,
	services wire.ServiceFlag) (*wire.NetAddress, error)

// MessageListeners defines callback function pointers to invoke with message
// listeners for a peer. Any listener which is not set to a concrete callback
// during peer initialization is ignored. Execution of multiple message
// listeners occurs serially, so one callback blocks the execution of the next.
//
// NOTE: Unless otherwise documented, these listeners must NOT directly call any
// blocking calls (such as WaitForShutdown) on the peer instance since the input
// handler goroutine blocks until the callback has completed.  Doing so will
// result in a deadlock.
type MessageListeners struct {

	/*
		// OnGetAddr is invoked when a peer receives a getaddr bitcoin message.
		OnGetAddr func(p *Peer, msg *wire.MsgGetAddr)

		// OnAddr is invoked when a peer receives an addr bitcoin message.
		OnAddr func(p *Peer, msg *wire.MsgAddr)

		// OnPing is invoked when a peer receives a ping bitcoin message.
		OnPing func(p *Peer, msg *wire.MsgPing)

		// OnPong is invoked when a peer receives a pong bitcoin message.
		OnPong func(p *Peer, msg *wire.MsgPong)

		// OnAlert is invoked when a peer receives an alert bitcoin message.
		OnAlert func(p *Peer, msg *wire.MsgAlert)

		// OnMemPool is invoked when a peer receives a mempool bitcoin message.
		OnMemPool func(p *Peer, msg *wire.MsgMemPool)

		// OnTx is invoked when a peer receives a tx bitcoin message.
		OnTx func(p *Peer, msg *wire.MsgTx)

		// OnBlock is invoked when a peer receives a block bitcoin message.
		OnBlock func(p *Peer, msg *wire.MsgBlock, buf []byte)

		// OnCFilter is invoked when a peer receives a cfilter bitcoin message.
		OnCFilter func(p *Peer, msg *wire.MsgCFilter)

		// OnCFHeaders is invoked when a peer receives a cfheaders bitcoin
		// message.
		OnCFHeaders func(p *Peer, msg *wire.MsgCFHeaders)

		// OnCFCheckpt is invoked when a peer receives a cfcheckpt bitcoin
		// message.
		OnCFCheckpt func(p *Peer, msg *wire.MsgCFCheckpt)

		// OnInv is invoked when a peer receives an inv bitcoin message.
		OnInv func(p *Peer, msg *wire.MsgInv)

		// OnHeaders is invoked when a peer receives a headers bitcoin message.
		OnHeaders func(p *Peer, msg *wire.MsgHeaders)

		// OnNotFound is invoked when a peer receives a notfound bitcoin
		// message.
		OnNotFound func(p *Peer, msg *wire.MsgNotFound)

		// OnGetData is invoked when a peer receives a getdata bitcoin message.
		OnGetData func(p *Peer, msg *wire.MsgGetData)

		// OnGetBlocks is invoked when a peer receives a getblocks bitcoin
		// message.
		OnGetBlocks func(p *Peer, msg *wire.MsgGetBlocks)

		// OnGetHeaders is invoked when a peer receives a getheaders bitcoin
		// message.
		OnGetHeaders func(p *Peer, msg *wire.MsgGetHeaders)

		// OnGetCFilters is invoked when a peer receives a getcfilters bitcoin
		// message.
		OnGetCFilters func(p *Peer, msg *wire.MsgGetCFilters)

		// OnGetCFHeaders is invoked when a peer receives a getcfheaders
		// bitcoin message.
		OnGetCFHeaders func(p *Peer, msg *wire.MsgGetCFHeaders)

		// OnGetCFCheckpt is invoked when a peer receives a getcfcheckpt
		// bitcoin message.
		OnGetCFCheckpt func(p *Peer, msg *wire.MsgGetCFCheckpt)

		// OnFeeFilter is invoked when a peer receives a feefilter bitcoin message.
		OnFeeFilter func(p *Peer, msg *wire.MsgFeeFilter)

		// OnFilterAdd is invoked when a peer receives a filteradd bitcoin message.
		OnFilterAdd func(p *Peer, msg *wire.MsgFilterAdd)

		// OnFilterClear is invoked when a peer receives a filterclear bitcoin
		// message.
		OnFilterClear func(p *Peer, msg *wire.MsgFilterClear)

		// OnFilterLoad is invoked when a peer receives a filterload bitcoin
		// message.
		OnFilterLoad func(p *Peer, msg *wire.MsgFilterLoad)

		// OnMerkleBlock  is invoked when a peer receives a merkleblock bitcoin
		// message.
		OnMerkleBlock func(p *Peer, msg *wire.MsgMerkleBlock)

		// OnVersion is invoked when a peer receives a version bitcoin message.
		// The caller may return a reject message in which case the message will
		// be sent to the peer and the peer will be disconnected.
		OnVersion func(p *Peer, msg *wire.MsgVersion) *wire.MsgReject

		// OnVerAck is invoked when a peer receives a verack bitcoin message.
		OnVerAck func(p *Peer, msg *wire.MsgVerAck)

		// OnReject is invoked when a peer receives a reject bitcoin message.
		OnReject func(p *Peer, msg *wire.MsgReject)

		// OnSendHeaders is invoked when a peer receives a sendheaders bitcoin
		// message.
		OnSendHeaders func(p *Peer, msg *wire.MsgSendHeaders)

		// OnRead is invoked when a peer receives a bitcoin message.  It
		// consists of the number of bytes read, the message, and whether or not
		// an error in the read occurred.  Typically, callers will opt to use
		// the callbacks for the specific message types, however this can be
		// useful for circumstances such as keeping track of server-wide byte
		// counts or working with custom message types for which the peer does
		// not directly provide a callback.
		OnRead func(p *Peer, bytesRead int, msg wire.Message, err error)

		// OnWrite is invoked when we write a bitcoin message to a peer.  It
		// consists of the number of bytes written, the message, and whether or
		// not an error in the write occurred.  This can be useful for
		// circumstances such as keeping track of server-wide byte counts.
		OnWrite func(p *Peer, bytesWritten int, msg wire.Message, err error)

	*/
}

// Config is the struct to hold configuration options useful to Peer.
type Config struct {
	// NewestBlock specifies a callback which provides the newest block
	// details to the peer as needed.  This can be nil in which case the
	// peer will report a block height of 0, however it is good practice for
	// peers to specify this so their currently best known is accurately
	// reported.
	NewestBlock HashFunc

	// HostToNetAddress returns the netaddress for the given host. This can be
	// nil in  which case the host will be parsed as an IP address.
	HostToNetAddress HostToNetAddrFunc

	// Proxy indicates a proxy is being used for connections.  The only
	// effect this has is to prevent leaking the tor proxy address, so it
	// only needs to specified if using a tor proxy.
	Proxy string

	// UserAgentName specifies the user agent name to advertise.  It is
	// highly recommended to specify this value.
	UserAgentName string

	// UserAgentVersion specifies the user agent version to advertise.  It
	// is highly recommended to specify this value and that it follows the
	// form "major.minor.revision" e.g. "2.6.41".
	UserAgentVersion string

	// UserAgentComments specify the user agent comments to advertise.  These
	// values must not contain the illegal characters specified in BIP 14:
	// '/', ':', '(', ')'.
	UserAgentComments []string

	// ChainParams identifies which chain parameters the peer is associated
	// with.  It is highly recommended to specify this field, however it can
	// be omitted in which case the test network will be used.
	ChainParams *chaincfg.Params

	// Services specifies which services to advertise as supported by the
	// local peer.  This field can be omitted in which case it will be 0
	// and therefore advertise no supported services.
	Services wire.ServiceFlag

	// ProtocolVersion specifies the maximum protocol version to use and
	// advertise.  This field can be omitted in which case
	// peer.MaxProtocolVersion will be used.
	ProtocolVersion uint32

	// DisableRelayTx specifies if the remote peer should be informed to
	// not send inv messages for transactions.
	DisableRelayTx bool

	// Listeners houses callback functions to be invoked on receiving peer
	// messages.
	Listeners MessageListeners

	// TrickleInterval is the duration of the ticker which trickles down the
	// inventory to a peer.
	TrickleInterval time.Duration

	// AllowSelfConns is only used to allow the tests to bypass the self
	// connection detecting and disconnect logic since they intentionally
	// do so for testing purposes.
	AllowSelfConns bool

	// DisableStallHandler if true, then the stall handler that attempts to
	// disconnect from peers that appear to be taking too long to respond
	// to requests won't be activated. This can be useful in certain simnet
	// scenarios where the stall behavior isn't important to the system
	// under test.
	DisableStallHandler bool
}

// outMsg is used to house a message to be sent along with a channel to signal
// when the message has been sent (or won't be sent due to things such as
// shutdown)
type outMsg struct {
	msg      wire.Message
	doneChan chan<- struct{}
	encoding wire.MessageEncoding
}

// NOTE: The overall data flow of a peer is split into 3 goroutines.  Inbound
// messages are read via the inHandler goroutine and generally dispatched to
// their own handler.  For inbound data-related messages such as blocks,
// transactions, and inventory, the data is handled by the corresponding
// message handlers.  The data flow for outbound messages is split into 2
// goroutines, queueHandler and outHandler.  The first, queueHandler, is used
// as a way for external entities to queue messages, by way of the QueueMessage
// function, quickly regardless of whether the peer is currently sending or not.
// It acts as the traffic cop between the external world and the actual
// goroutine which writes to the network socket.

// Peer provides a basic concurrent safe bitcoin peer for handling bitcoin
// communications via the peer-to-peer protocol.  It provides full duplex
// reading and writing, automatic handling of the initial handshake process,
// querying of usage statistics and other information about the remote peer such
// as its address, user agent, and protocol version, output message queuing,
// inventory trickling, and the ability to dynamically register and unregister
// callbacks for handling bitcoin protocol messages.
//
// Outbound messages are typically queued via QueueMessage or QueueInventory.
// QueueMessage is intended for all messages, including responses to data such
// as blocks and transactions.  QueueInventory, on the other hand, is only
// intended for relaying inventory as it employs a trickling mechanism to batch
// the inventory together.  However, some helper functions for pushing messages
// of specific types that typically require common special handling are
// provided as a convenience.
type Peer struct {
	// The following variables must only be used atomically.
	bytesReceived uint64
	bytesSent     uint64
	lastRecv      int64
	lastSend      int64
	connected     int32
	disconnect    int32

	conn net.Conn

	// These fields are set at creation time and never modified, so they are
	// safe to read from concurrently without a mutex.
	addr    string
	cfg     Config
	inbound bool

	flagsMtx             sync.Mutex // protects the peer flags below
	na                   *wire.NetAddress
	id                   int32
	userAgent            string
	services             wire.ServiceFlag
	versionKnown         bool
	advertisedProtoVer   uint32 // protocol version advertised by remote
	protocolVersion      uint32 // negotiated protocol version
	sendHeadersPreferred bool   // peer sent a sendheaders message
	verAckReceived       bool
	witnessEnabled       bool

	wireEncoding wire.MessageEncoding

	knownInventory     lru.Cache
	prevGetBlocksMtx   sync.Mutex
	prevGetBlocksBegin *chainhash.Hash
	prevGetBlocksStop  *chainhash.Hash
	prevGetHdrsMtx     sync.Mutex
	prevGetHdrsBegin   *chainhash.Hash
	prevGetHdrsStop    *chainhash.Hash

	// These fields keep track of statistics for the peer and are protected
	// by the statsMtx mutex.
	statsMtx           sync.RWMutex
	timeOffset         int64
	timeConnected      time.Time
	startingHeight     int32
	lastBlock          int32
	lastAnnouncedBlock *chainhash.Hash
	lastPingNonce      uint64    // Set to nonce if we have a pending ping.
	lastPingTime       time.Time // Time we sent last ping.
	lastPingMicros     int64     // Time for last ping to return.

	stallControl  chan stallControlMsg
	outputQueue   chan outMsg
	sendQueue     chan outMsg
	sendDoneQueue chan struct{}
	outputInvChan chan *wire.InvVect
	inQuit        chan struct{}
	queueQuit     chan struct{}
	outQuit       chan struct{}
	quit          chan struct{}
}
