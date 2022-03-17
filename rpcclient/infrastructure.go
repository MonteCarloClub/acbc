package rpcclient

import (
	"container/list"
	"errors"
	"github.com/MonteCarloClub/acbc/acbcjson"
	"github.com/MonteCarloClub/acbc/chaincfg"
	"github.com/btcsuite/btcd/btcjson"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/btcsuite/websocket"
)

//定义client的基础结构

var (
	// ErrInvalidAuth is an error to describe the condition where the client
	// is either unable to authenticate or the specified endpoint is
	// incorrect.
	ErrInvalidAuth = errors.New("authentication failure")

	// ErrInvalidEndpoint is an error to describe the condition where the
	// websocket handshake failed with the specified endpoint.
	ErrInvalidEndpoint = errors.New("the endpoint either does not support " +
		"websockets or does not exist")

	// ErrClientNotConnected is an error to describe the condition where a
	// websocket client has been created, but the connection was never
	// established.  This condition differs from ErrClientDisconnect, which
	// represents an established connection that was lost.
	ErrClientNotConnected = errors.New("the client was never connected")

	// ErrClientDisconnect is an error to describe the condition where the
	// client has been disconnected from the RPC server.  When the
	// DisableAutoReconnect option is not set, any outstanding futures
	// when a client disconnect occurs will return this error as will
	// any new requests.
	ErrClientDisconnect = errors.New("the client has been disconnected")

	// ErrClientShutdown is an error to describe the condition where the
	// client is either already shutdown, or in the process of shutting
	// down.  Any outstanding futures when a client shutdown occurs will
	// return this error as will any new requests.
	ErrClientShutdown = errors.New("the client has been shutdown")

	// ErrNotWebsocketClient is an error to describe the condition of
	// calling a Client method intended for a websocket client when the
	// client has been configured to run in HTTP POST mode instead.
	ErrNotWebsocketClient = errors.New("client is not configured for " +
		"websockets")

	// ErrClientAlreadyConnected is an error to describe the condition where
	// a new client connection cannot be established due to a websocket
	// client having already connected to the RPC server.
	ErrClientAlreadyConnected = errors.New("websocket client has already " +
		"connected")
)

// Client represents a Bitcoin RPC client which allows easy access to the
// various RPC methods available on a Bitcoin RPC server.  Each of the wrapper
// functions handle the details of converting the passed and return types to and
// from the underlying JSON types which are required for the JSON-RPC
// invocations

// ConnConfig describes the connection configuration parameters for the client.
type ConnConfig struct {
	// Host is the IP address and port of the RPC server you want to connect
	// to.
	Host string

	// Endpoint is the websocket endpoint on the RPC server.  This is
	// typically "ws".
	Endpoint string

	// User is the username to use to authenticate to the RPC server.
	User string

	// Pass is the passphrase to use to authenticate to the RPC server.
	Pass string

	// CookiePath is the path to a cookie file containing the username and
	// passphrase to use to authenticate to the RPC server.  It is used
	// instead of User and Pass if non-empty.
	CookiePath string

	cookieLastCheckTime time.Time
	cookieLastModTime   time.Time
	cookieLastUser      string
	cookieLastPass      string
	cookieLastErr       error

	// Params is the string representing the network that the server
	// is running. If there is no parameter set in the config, then
	// mainnet will be used by default.
	Params string

	// DisableTLS specifies whether transport layer security should be
	// disabled.  It is recommended to always use TLS if the RPC server
	// supports it as otherwise your username and password is sent across
	// the wire in cleartext.
	DisableTLS bool

	// Certificates are the bytes for a PEM-encoded certificate chain used
	// for the TLS connection.  It has no effect if the DisableTLS parameter
	// is true.
	Certificates []byte

	// Proxy specifies to connect through a SOCKS 5 proxy server.  It may
	// be an empty string if a proxy is not required.
	Proxy string

	// ProxyUser is an optional username to use for the proxy server if it
	// requires authentication.  It has no effect if the Proxy parameter
	// is not set.
	ProxyUser string

	// ProxyPass is an optional password to use for the proxy server if it
	// requires authentication.  It has no effect if the Proxy parameter
	// is not set.
	ProxyPass string

	// DisableAutoReconnect specifies the client should not automatically
	// try to reconnect to the server when it has been disconnected.
	DisableAutoReconnect bool

	// DisableConnectOnNew specifies that a websocket client connection
	// should not be tried when creating the client with New.  Instead, the
	// client is created and returned unconnected, and Connect must be
	// called manually.
	DisableConnectOnNew bool

	// HTTPPostMode instructs the client to run using multiple independent
	// connections issuing HTTP POST requests instead of using the default
	// of websockets.  Websockets are generally preferred as some of the
	// features of the client such notifications only work with websockets,
	// however, not all servers support the websocket extensions, so this
	// flag can be set to true to use basic HTTP POST requests instead.
	HTTPPostMode bool

	// ExtraHeaders specifies the extra headers when perform request. It's
	// useful when RPC provider need customized headers.
	ExtraHeaders map[string]string

	// EnableBCInfoHacks is an option provided to enable compatibility hacks
	// when connecting to blockchain.info RPC server
	EnableBCInfoHacks bool
}

// BackendVersion represents the version of the backend the client is currently
// connected to.
type BackendVersion uint8

// notificationState is used to track the current state of successfully
// registered notification so the state can be automatically re-established on
// reconnect.
type notificationState struct {
	notifyBlocks       bool
	notifyNewTx        bool
	notifyNewTxVerbose bool
	notifyReceived     map[string]struct{}
	notifySpent        map[btcjson.OutPoint]struct{}
}

// jsonRequest holds information about a json request that is used to properly
// detect, interpret, and deliver a reply to it.
type jsonRequest struct {
	id             uint64
	method         string
	cmd            interface{}
	marshalledJSON []byte
	responseChan   chan *Response
}

//
// The client provides each RPC in both synchronous (blocking) and asynchronous
// (non-blocking) forms.  The asynchronous forms are based on the concept of
// futures where they return an instance of a type that promises to deliver the
// result of the invocation at some future time.  Invoking the Receive method on
// the returned future will block until the result is available if it's not
// already.

//客户端以同步(阻塞)和异步(非阻塞)的形式提供每个RPC。异步表单基于future的概念，
//其中它们返回一个类型的实例，该实例承诺在未来某个时间交付调用的结果。在返回的future上
//调用Receive方法将会阻塞，直到结果可用(如果还没有可用的话)。

type Client struct {
	id uint64 // atomic, so must stay 64-bit aligned

	// config holds the connection configuration assoiated with this client.
	config *ConnConfig

	// chainParams holds the params for the chain that this client is using,
	// and is used for many wallet methods.
	chainParams *chaincfg.Params

	// wsConn is the underlying websocket connection when not in HTTP POST
	// mode.
	wsConn *websocket.Conn

	// httpClient is the underlying HTTP client to use when running in HTTP
	// POST mode.
	httpClient *http.Client

	// backendVersion is the version of the backend the client is currently
	// connected to. This should be retrieved through GetVersion.
	backendVersionMu sync.Mutex
	backendVersion   *BackendVersion

	// mtx is a mutex to protect access to connection related fields.
	mtx sync.Mutex

	// disconnected indicated whether or not the server is disconnected.
	disconnected bool

	// whether or not to batch requests, false unless changed by Batch()
	batch     bool
	batchList *list.List

	// retryCount holds the number of times the client has tried to
	// reconnect to the RPC server.
	retryCount int64

	// Track command and their response channels by ID.
	requestLock sync.Mutex
	requestMap  map[uint64]*list.Element
	requestList *list.List

	// Notifications.
	ntfnHandlers  *NotificationHandlers
	ntfnStateLock sync.Mutex
	ntfnState     *notificationState

	// Networking infrastructure.
	sendChan        chan []byte
	sendPostChan    chan *jsonRequest
	connEstablished chan struct{}
	disconnect      chan struct{}
	shutdown        chan struct{}
	wg              sync.WaitGroup
}

// NextID returns the next id to be used when sending a JSON-RPC message.  This
// ID allows responses to be associated with particular requests per the
// JSON-RPC specification.  Typically the consumer of the client does not need
// to call this function, however, if a custom request is being created and used
// this function should be used to ensure the ID is unique amongst all requests
// being made.
// NextID 返回发送 JSON-RPC 消息时要使用的下一个 id。此 ID 允许响应与每个 JSON-RPC 规范的特定请求相关联。
// 通常，客户端的消费者不需要调用此函数，但是，如果正在创建和使用自定义请求，则应使用此函数来确保 ID 在所有发出的请求中是唯一的
func (c *Client) NextID() uint64 {
	return atomic.AddUint64(&c.id, 1)
}

// addRequest associates the passed jsonRequest with its id.  This allows the
// response from the remote server to be unmarshalled to the appropriate type
// and sent to the specified channel when it is received.
//
// If the client has already begun shutting down, ErrClientShutdown is returned
// and the request is not added.
//
// This function is safe for concurrent access.
// addRequest 将传递的 jsonRequest 与其 id 相关联。这允许将来自远程服务器的响应解组为适当的类型，
// 并在收到时发送到指定的通道。如果客户端已经开始关闭，则返回 ErrClientShutdown 并且不添加请求。该函数对并发访问是安全的
func (c *Client) addRequest(jReq *jsonRequest) error {
	// todo： 有待具体分析
	c.requestLock.Lock()
	defer c.requestLock.Unlock()

	// A non-blocking read of the shutdown channel with the request lock
	// held avoids adding the request to the client's internal data
	// structures if the client is in the process of shutting down (and
	// has not yet grabbed the request lock), or has finished shutdown
	// already (responding to each outstanding request with
	// ErrClientShutdown).

	//如果客户端正在关闭过程中（并且尚未获取请求锁），或者已经完成关闭，则在持有请求锁的情况下
	//对关闭通道进行非阻塞读取可避免将请求添加到客户端的内部数据结构中（使用 ErrClientShutdown 响应每个未完成的请求）。
	select {
	case <-c.shutdown:
		return ErrClientShutdown
	default:
	}

	if !c.batch {
		element := c.requestList.PushBack(jReq)
		c.requestMap[jReq.id] = element
	} else {
		element := c.batchList.PushBack(jReq)
		c.requestMap[jReq.id] = element
	}
	return nil
}

// sendPostRequest sends the passed HTTP request to the RPC server using the
// HTTP client associated with the client.  It is backed by a buffered channel,
// so it will not block until the send channel is full.
// sendPostRequest 使用与客户端关联的 HTTP 客户端将传递的 HTTP 请求发送到 RPC 服务器。
// 它由缓冲通道支持，因此在发送通道满之前不会阻塞
func (c *Client) sendPostRequest(jReq *jsonRequest) {
	// todo： 有待具体分析
	// Don't send the message if shutting down.
	// q： 为什么要使用一个channel c.shutdown来判断 是否关闭 而不是一个 bool型
	select {
	case <-c.shutdown:
		jReq.responseChan <- &Response{result: nil, err: ErrClientShutdown}
	default:
	}

	log.Tracef("Sending command [%s] with id %d", jReq.method, jReq.id)
	// q：分析这里的 <-
	c.sendPostChan <- jReq
}

// disconnectChan returns a copy of the current disconnect channel.  The channel
// is read protected by the client mutex, and is safe to call while the channel
// is being reassigned during a reconnect.
func (c *Client) disconnectChan() <-chan struct{} {
	c.mtx.Lock()
	ch := c.disconnect
	c.mtx.Unlock()
	return ch
}

// sendMessage sends the passed JSON to the connected server using the
// websocket connection.  It is backed by a buffered channel, so it will not
// block until the send channel is full.
// sendMessage 使用 websocket 连接将传递的 JSON 发送到连接的服务器。它由缓冲通道支持，因此在发送通道满之前不会阻塞。
func (c *Client) sendMessage(marshalledJSON []byte) {
	// Don't send the message if disconnected.
	// todo ： 待具体分析
	select {
	case c.sendChan <- marshalledJSON:
	case <-c.disconnectChan():
		return
	}
}

// sendRequest sends the passed json request to the associated server using the
// provided response channel for the reply.  It handles both websocket and HTTP
// POST mode depending on the configuration of the client.
// sendRequest 使用提供的响应channel 将传递的 json 请求发送到关联的服务器以等待回复。它根据客户
// 端的配置可以处理 websocket 和 HTTP POST 模式。
func (c *Client) sendRequest(jReq *jsonRequest) {
	// Choose which marshal and send function to use depending on whether
	// the client running in HTTP POST mode or not.  When running in HTTP
	// POST mode, the command is issued via an HTTP client.  Otherwise,
	// the command is issued via the asynchronous websocket channels.
	if c.config.HTTPPostMode {
		if c.batch {
			if err := c.addRequest(jReq); err != nil {
				log.Warn(err)
			}
		} else {
			c.sendPostRequest(jReq)
		}
		return
	}

	// Check whether the websocket connection has never been established,
	// in which case the handler goroutines are not running.
	select {
	case <-c.connEstablished:
	default:
		jReq.responseChan <- &Response{err: ErrClientNotConnected}
		return
	}

	// Add the request to the internal tracking map so the response from the
	// remote server can be properly detected and routed to the response
	// channel.  Then send the marshalled request via the websocket
	// connection.
	if err := c.addRequest(jReq); err != nil {
		jReq.responseChan <- &Response{err: err}
		return
	}
	log.Tracef("Sending command [%s] with id %d", jReq.method, jReq.id)
	c.sendMessage(jReq.marshalledJSON)
}

// newFutureError returns a new future result channel that already has the
// passed error waitin on the channel with the reply set to nil.  This is useful
// to easily return errors from the various Async functions.
func newFutureError(err error) chan *Response {
	responseChan := make(chan *Response, 1)
	responseChan <- &Response{err: err}
	return responseChan
}

// SendCmd sends the passed command to the associated server and returns a
// response channel on which the reply will be delivered at some point in the
// future.  It handles both websocket and HTTP POST mode depending on the
// configuration of the client.
func (c *Client) SendCmd(cmd interface{}) chan *Response {
	rpcVersion := acbcjson.RpcVersion1
	if c.batch {
		rpcVersion = acbcjson.RpcVersion2
	}
	// Get the method associated with the command.
	method, err := acbcjson.CmdMethod(cmd)
	if err != nil {
		return newFutureError(err)
	}

	// Marshal the command.
	// q：为什么要发送一个自增的id？？？
	// r：与批量发送请求有关
	id := c.NextID()
	marshalledJSON, err := acbcjson.MarshalCmd(rpcVersion, id, cmd)
	if err != nil {
		return newFutureError(err)
	}

	// Generate the request and send it along with a channel to respond on.
	responseChan := make(chan *Response, 1)
	// q： 这里和MarshalCmd中的&Request有什么区别
	jReq := &jsonRequest{
		id:             id,
		method:         method,
		cmd:            cmd,
		marshalledJSON: marshalledJSON,
		responseChan:   responseChan,
	}

	c.sendRequest(jReq)

	return responseChan
}

// ReceiveFuture receives from the passed futureResult channel to extract a
// reply or any errors.  The examined errors include an error in the
// futureResult and the error in the reply from the server.  This will block
// until the result is available on the passed channel.
func ReceiveFuture(f chan *Response) ([]byte, error) {
	// Wait for a response on the returned channel.
	r := <-f
	return r.result, r.err
}
