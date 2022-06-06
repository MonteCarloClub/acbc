package rpcclient

import (
	"bytes"
	"container/list"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MonteCarloClub/acbc/acbcjson"
	"github.com/MonteCarloClub/acbc/log"
	"github.com/btcsuite/btclog"
	"io"
	"io/ioutil"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

//定义client的基础结构

// log is a logger that is initialized with no output filters.  This
// means the package will not perform any logging by default until the caller
// requests it.
var rpcclientlog = btclog.Disabled

var (

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
)

const (
	// sendBufferSize is the number of elements the websocket send channel
	// can queue before blocking.
	sendBufferSize = 50

	// sendPostBufferSize is the number of elements the HTTP POST send
	// channel can queue before blocking.
	sendPostBufferSize = 100

	// requestRetryInterval is the initial amount of time to wait in between
	// retries when sending HTTP POST requests.
	requestRetryInterval = time.Millisecond * 500
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

// jsonRequest holds information about a json request that is used to properly
// detect, interpret, and deliver a reply to it.
type jsonRequest struct {
	id             uint64
	marshalledJSON []byte
	responseChan   chan *Response
}

// inMessage is the first type that an incoming message is unmarshaled
// into. It supports both requests (for notification support) and
// responses.  The partially-unmarshaled message is a notification if
// the embedded ID (from the response) is nil.  Otherwise, it is a
// response.
type inMessage struct {
	ID *float64 `json:"id"`
	*rawNotification
	*rawResponse
}

// rawNotification is a partially-unmarshaled JSON-RPC notification.
type rawNotification struct {
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Response is the raw bytes of a JSON-RPC result, or the error if the response
// error object was non-null.
type Response struct {
	ModuleFrom string          `json:"modulefrom"`
	ModuleTo   string          `json:"moduleto"`
	Result     json.RawMessage `json:"result"`
	Error      error           `json:"error"`
	ID         *interface{}    `json:"id"`
}

// rawResponse is a partially-unmarshaled JSON-RPC response.  For this
// to be valid (according to JSON-RPC 1.0 spec), ID may not be nil.
type rawResponse struct {
	ModuleFrom string             `json:"modulefrom"`
	ModuleTo   string             `json:"moduleto"`
	Result     json.RawMessage    `json:"result"`
	Error      *acbcjson.RPCError `json:"error"`
	ID         *interface{}       `json:"id"`
}

// result checks whether the unmarshaled response contains a non-nil error,
// returning an unmarshaled acbcjson.RPCError (or an unmarshaling error) if so.
// If the response is not an error, the raw bytes of the request are
// returned for further unmashaling into specific result types.
func (r rawResponse) result() (result []byte, err error) {
	if r.Error != nil {
		return nil, r.Error
	}
	return r.Result, nil
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

	// Networking infrastructure.
	sendChan        chan []byte
	sendPostChan    chan *jsonRequest
	connEstablished chan struct{}
	disconnect      chan struct{}
	shutdown        chan struct{}
	wg              sync.WaitGroup
}

// getAuth returns the username and passphrase that will actually be used for
// this connection.  This will be the result of checking the cookie if a cookie
// path is configured; if not, it will be the user-configured username and
// passphrase.
func (config *ConnConfig) getAuth() (username, passphrase string, err error) {
	// Try username+passphrase auth first.
	if config.Pass != "" {
		return config.User, config.Pass, nil
	}

	// If no username or passphrase is set, try cookie auth.
	return config.retrieveCookie()
}

// retrieveCookie returns the cookie username and passphrase.
func (config *ConnConfig) retrieveCookie() (username, passphrase string, err error) {
	if !config.cookieLastCheckTime.IsZero() && time.Now().Before(config.cookieLastCheckTime.Add(30*time.Second)) {
		return config.cookieLastUser, config.cookieLastPass, config.cookieLastErr
	}

	config.cookieLastCheckTime = time.Now()

	st, err := os.Stat(config.CookiePath)
	if err != nil {
		config.cookieLastErr = err
		return config.cookieLastUser, config.cookieLastPass, config.cookieLastErr
	}

	modTime := st.ModTime()
	if !modTime.Equal(config.cookieLastModTime) {
		config.cookieLastModTime = modTime
		config.cookieLastUser, config.cookieLastPass, config.cookieLastErr = readCookieFile(config.CookiePath)
	}

	return config.cookieLastUser, config.cookieLastPass, config.cookieLastErr
}

// newHTTPClient returns a new http client that is configured according to the
// proxy and TLS settings in the associated connection configuration.
func newHTTPClient(config *ConnConfig) (*http.Client, error) {
	// Set proxy function if there is a proxy configured.
	var proxyFunc func(*http.Request) (*url.URL, error)
	if config.Proxy != "" {
		proxyURL, err := url.Parse(config.Proxy)
		if err != nil {
			return nil, err
		}
		proxyFunc = http.ProxyURL(proxyURL)
	}

	// Configure TLS if needed.
	var tlsConfig *tls.Config
	if !config.DisableTLS {
		if len(config.Certificates) > 0 {
			pool := x509.NewCertPool()
			pool.AppendCertsFromPEM(config.Certificates)
			tlsConfig = &tls.Config{
				RootCAs: pool,
			}
		}
	}

	client := http.Client{
		Transport: &http.Transport{
			Proxy:           proxyFunc,
			TLSClientConfig: tlsConfig,
		},
	}

	return &client, nil
}

// doDisconnect disconnects the websocket associated with the client if it
// hasn't already been disconnected.  It will return false if the disconnect is
// not needed or the client is running in HTTP POST mode.
//
// This function is safe for concurrent access.
func (c *Client) doDisconnect() bool {
	fmt.Println("dodisconnect2222")
	if c.config.HTTPPostMode {
		return false
	}

	c.mtx.Lock()
	defer c.mtx.Unlock()

	// Nothing to do if already disconnected.
	if c.disconnected {
		return false
	}

	rpcclientlog.Tracef("Disconnecting RPC client %s", c.config.Host)
	close(c.disconnect)
	c.disconnected = true
	return true
}

// removeRequest returns and removes the jsonRequest which contains the response
// channel and original method associated with the passed id or nil if there is
// no association.
//
// This function is safe for concurrent access.
func (c *Client) removeRequest(id uint64) *jsonRequest {
	c.requestLock.Lock()
	defer c.requestLock.Unlock()

	element := c.requestMap[id]
	if element != nil {
		delete(c.requestMap, id)
		request := c.requestList.Remove(element).(*jsonRequest)
		return request
	}

	return nil
}

// handleMessage is the main handler for incoming notifications and responses.
func (c *Client) handleMessage(msg []byte) {
	// Attempt to unmarshal the message as either a notification or
	// response.
	var in inMessage
	in.rawResponse = new(rawResponse)
	in.rawNotification = new(rawNotification)
	err := json.Unmarshal(msg, &in)
	if err != nil {
		rpcclientlog.Warnf("Remote server sent invalid message: %v", err)
		return
	}

	// JSON-RPC 1.0 notifications are requests with a null id.
	if in.ID == nil {
		ntfn := in.rawNotification
		if ntfn == nil {
			rpcclientlog.Warn("Malformed notification: missing " +
				"method and parameters")
			return
		}
		if ntfn.Method == "" {
			rpcclientlog.Warn("Malformed notification: missing method")
			return
		}
		// params are not optional: nil isn't valid (but len == 0 is)
		if ntfn.Params == nil {
			rpcclientlog.Warn("Malformed notification: missing params")
			return
		}
		// Deliver the notification.
		rpcclientlog.Tracef("Received notification [%s]", in.Method)

		return
	}

	// ensure that in.ID can be converted to an integer without loss of precision
	if *in.ID < 0 || *in.ID != math.Trunc(*in.ID) {
		rpcclientlog.Warn("Malformed response: invalid identifier")
		return
	}

	if in.rawResponse == nil {
		rpcclientlog.Warn("Malformed response: missing result and error")
		return
	}

	id := uint64(*in.ID)
	rpcclientlog.Tracef("Received response for id %d (result %s)", id, in.Result)
	request := c.removeRequest(id)

	// Nothing more to do if there is no request associated with this reply.
	if request == nil || request.responseChan == nil {
		rpcclientlog.Warnf("Received unexpected reply: %s (id %d)", in.Result,
			id)
		return
	}

	// Deliver the response.
	result, err := in.rawResponse.result()
	request.responseChan <- &Response{Result: result, Error: err}
}

// shouldLogReadError returns whether or not the passed error, which is expected
// to have come from reading from the websocket connection in wsInHandler,
// should be logged.
func (c *Client) shouldLogReadError(err error) bool {
	// No logging when the connetion is being forcibly disconnected.
	select {
	case <-c.shutdown:
		fmt.Println(5555555)
		return false
	default:
	}

	// No logging when the connection has been disconnected.
	if err == io.EOF {
		return false
	}
	if opErr, ok := err.(*net.OpError); ok && !opErr.Temporary() {
		return false
	}

	return true
}

// handleSendPostMessage handles performing the passed HTTP request, reading the
// result, unmarshalling it, and delivering the unmarshalled result to the
// provided response channel.
func (c *Client) handleSendPostMessage(jReq *jsonRequest) {
	protocol := "http"
	if !c.config.DisableTLS {
		protocol = "https"
	}
	url := protocol + "://" + c.config.Host

	var err error
	var backoff time.Duration
	var httpResponse *http.Response
	tries := 10
	for i := 0; tries == 0 || i < tries; i++ {
		bodyReader := bytes.NewReader(jReq.marshalledJSON)
		httpReq, err := http.NewRequest("POST", url, bodyReader)
		if err != nil {
			jReq.responseChan <- &Response{Result: nil, Error: err}
			return
		}
		httpReq.Close = true
		httpReq.Header.Set("Content-Type", "application/json")
		for key, value := range c.config.ExtraHeaders {
			httpReq.Header.Set(key, value)
		}

		// Configure basic access authorization.
		user, pass, err := c.config.getAuth()
		if err != nil {
			jReq.responseChan <- &Response{Result: nil, Error: err}
			return
		}
		httpReq.SetBasicAuth(user, pass)

		httpResponse, err = c.httpClient.Do(httpReq)
		if err != nil {
			backoff = requestRetryInterval * time.Duration(i+1)
			if backoff > time.Minute {
				backoff = time.Minute
			}
			rpcclientlog.Debugf("Failed command with id %d attempt %d. Retrying in %v... \n", jReq.id, i, backoff)
			time.Sleep(backoff)
			continue
		}
		break
	}
	if err != nil {
		jReq.responseChan <- &Response{Error: err}
		return
	}

	// Read the raw bytes and close the response.
	respBytes, err := ioutil.ReadAll(httpResponse.Body)
	httpResponse.Body.Close()
	if err != nil {
		err = fmt.Errorf("error reading json reply: %v", err)
		jReq.responseChan <- &Response{Error: err}
		return
	}

	// Try to unmarshal the response as a regular JSON-RPC response.
	var resp rawResponse
	var batchResponse json.RawMessage
	if c.batch {
		err = json.Unmarshal(respBytes, &batchResponse)
	} else {
		err = json.Unmarshal(respBytes, &resp)
	}
	if err != nil {
		// When the response itself isn't a valid JSON-RPC response
		// return an error which includes the HTTP status code and raw
		// response bytes.
		err = fmt.Errorf("status code: %d, response: %q",
			httpResponse.StatusCode, string(respBytes))
		jReq.responseChan <- &Response{Error: err}
		return
	}
	var res []byte
	if c.batch {
		// errors must be dealt with downstream since a whole request cannot
		// "error out" other than through the status code error handled above
		res, err = batchResponse, nil
	} else {
		res, err = resp.result()
		fmt.Printf("%+v", res)
	}
	jReq.responseChan <- &Response{ModuleFrom: resp.ModuleFrom, ModuleTo: resp.ModuleTo, Result: res, Error: err, ID: resp.ID}
}

// sendPostHandler handles all outgoing messages when the client is running
// in HTTP POST mode.  It uses a buffered channel to serialize output messages
// while allowing the sender to continue running asynchronously.  It must be run
// as a goroutine.
func (c *Client) sendPostHandler() {
out:
	for {
		// Send any messages ready for send until the shutdown channel
		// is closed.
		select {
		case jReq := <-c.sendPostChan:
			c.handleSendPostMessage(jReq)

		case <-c.shutdown:
			break out
		}
	}

	// Drain any wait channels before exiting so nothing is left waiting
	// around to send.
cleanup:
	for {
		select {
		case jReq := <-c.sendPostChan:
			fmt.Println(0)
			jReq.responseChan <- &Response{
				Result: nil,
				Error:  ErrClientShutdown,
			}

		default:
			break cleanup
		}
	}
	c.wg.Done()
	rpcclientlog.Tracef("RPC client send handler done for %s", c.config.Host)

}

// start begins processing input and output messages.
func (c *Client) Start() {
	rpcclientlog.Tracef("Starting RPC client %s", c.config.Host)
	c.disconnected = false
	c.disconnect = make(chan struct{})
	c.shutdown = make(chan struct{})

	// Start the I/O processing handlers depending on whether the client is
	// in HTTP POST mode or the default websocket mode.
	if c.config.HTTPPostMode {
		c.wg.Add(1)
		go c.sendPostHandler()
	} else {
		rpcclientlog.Tracef("不支持非http")
		return
	}

}

// Disconnected returns whether or not the server is disconnected.  If a
// websocket client was created but never connected, this also returns false.
func (c *Client) Disconnected() bool {
	fmt.Println("Disconnected")
	c.mtx.Lock()
	defer c.mtx.Unlock()

	select {
	case <-c.connEstablished:
		return c.disconnected
	default:
		return false
	}
}

// removeAllRequests removes all the jsonRequests which contain the response
// channels for outstanding requests.
//
// This function MUST be called with the request lock held.
func (c *Client) removeAllRequests() {
	c.requestMap = make(map[uint64]*list.Element)
	c.requestList.Init()
}

// doShutdown closes the shutdown channel and logs the shutdown unless shutdown
// is already in progress.  It will return false if the shutdown is not needed.
//
// This function is safe for concurrent access.
func (c *Client) doShutdown() bool {
	// Ignore the shutdown request if the client is already in the process
	// of shutting down or already shutdown.
	select {
	case <-c.shutdown:
		return false
	default:
	}

	rpcclientlog.Tracef("Shutting down RPC client %s", c.config.Host)
	close(c.shutdown)
	return true
}

// Disconnect disconnects the current websocket associated with the client.  The
// connection will automatically be re-established unless the client was
// created with the DisableAutoReconnect flag.
//
// This function has no effect when the client is running in HTTP POST mode.
func (c *Client) Disconnect() {
	// Nothing to do if already disconnected or running in HTTP POST mode.
	if !c.doDisconnect() {
		return
	}

	c.requestLock.Lock()
	defer c.requestLock.Unlock()

	// When operating without auto reconnect, send errors to any pending
	// requests and shutdown the client.
	if c.config.DisableAutoReconnect {
		for e := c.requestList.Front(); e != nil; e = e.Next() {
			req := e.Value.(*jsonRequest)
			req.responseChan <- &Response{
				Result: nil,
				Error:  ErrClientDisconnect,
			}
		}
		c.removeAllRequests()
		c.doShutdown()
	}
}

// Shutdown shuts down the client by disconnecting any connections associated
// with the client and, when automatic reconnect is enabled, preventing future
// attempts to reconnect.  It also stops all goroutines.
func (c *Client) Shutdown() {
	// Do the shutdown under the request lock to prevent clients from
	// adding new requests while the client shutdown process is initiated.
	c.requestLock.Lock()
	defer c.requestLock.Unlock()

	// Ignore the shutdown request if the client is already in the process
	// of shutting down or already shutdown.
	if !c.doShutdown() {
		return
	}
	i := 1
	// Send the ErrClientShutdown error to any pending requests.
	for e := c.requestList.Front(); e != nil; e = e.Next() {
		req := e.Value.(*jsonRequest)
		fmt.Println(i)
		i = i + 1
		req.responseChan <- &Response{
			Result: nil,
			Error:  ErrClientShutdown,
		}
	}
	c.removeAllRequests()

	// Disconnect the client if needed.
	c.doDisconnect()
}

// New creates a new RPC client based on the provided connection configuration
// details.  The notification handlers parameter may be nil if you are not
// interested in receiving notifications and will be ignored if the
// configuration is set to run in HTTP POST mode.
func New(config *ConnConfig) (*Client, error) {
	// Either open a websocket connection or create an HTTP client depending
	// on the HTTP POST mode.  Also, set the notification handlers to nil
	// when running in HTTP POST mode.

	var httpClient *http.Client
	connEstablished := make(chan struct{})
	var start bool
	if config.HTTPPostMode {

		start = true

		var err error
		httpClient, err = newHTTPClient(config)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, errors.New("不支持非http")
	}

	client := &Client{
		config:          config,
		httpClient:      httpClient,
		requestMap:      make(map[uint64]*list.Element),
		requestList:     list.New(),
		batch:           false,
		batchList:       list.New(),
		sendChan:        make(chan []byte, sendBufferSize),
		sendPostChan:    make(chan *jsonRequest, sendPostBufferSize),
		connEstablished: connEstablished,
		disconnect:      make(chan struct{}),
		shutdown:        make(chan struct{}),
	}
	if start {
		rpcclientlog.Infof("Established connection to RPC server %s", config.Host)
		close(connEstablished)
		client.Start()
	}
	return client, nil
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
		jReq.responseChan <- &Response{Result: nil, Error: ErrClientShutdown}
	default:
	}

	rpcclientlog.Tracef("Sending with id %d", jReq.id)
	// q：分析这里的 <-
	c.sendPostChan <- jReq
}

// disconnectChan returns a copy of the current disconnect channel.  The channel
// is read protected by the client mutex, and is safe to call while the channel
// is being reassigned during a reconnect.
func (c *Client) disconnectChan() <-chan struct{} {
	fmt.Println("disconnectChan")
	c.mtx.Lock()
	ch := c.disconnect
	c.mtx.Unlock()
	return ch
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
				rpcclientlog.Warn(err)
			}
		} else {
			fmt.Println("..............sendPostRequest")
			c.sendPostRequest(jReq)
		}
		return
	}
}

// newFutureError returns a new future result channel that already has the
// passed error waitin on the channel with the reply set to nil.  This is useful
// to easily return errors from the various Async functions.
func newFutureError(err error) chan *Response {
	responseChan := make(chan *Response, 1)
	responseChan <- &Response{Error: err}
	return responseChan
}

// SendCmd sends the passed command to the associated server and returns a
// response channel on which the reply will be delivered at some point in the
// future.  It handles both websocket and HTTP POST mode depending on the
// configuration of the client.
func (c *Client) SendCmd(moduleFrom string, moduleTo string, cmd interface{}) chan *Response {
	// Get the method associated with the command.
	//method, err := acbcjson.CmdMethod(cmd)
	_, err := acbcjson.CmdMethod(cmd)
	if err != nil {
		return newFutureError(err)
	}

	// Marshal the command.
	// q：为什么要发送一个自增的id？？？
	// r：与批量发送请求有关
	id := c.NextID()
	marshalledJSON, err := acbcjson.MarshalCmd(moduleFrom, moduleTo, id, cmd)
	if err != nil {
		return newFutureError(err)
	}

	// Generate the request and send it along with a channel to respond on.
	responseChan := make(chan *Response, 1)
	// q： 这里和MarshalCmd中的&Request有什么区别
	jReq := &jsonRequest{
		id: id,
		//method:         method,
		//cmd:            cmd,
		marshalledJSON: marshalledJSON,
		responseChan:   responseChan,
	}

	c.sendRequest(jReq)

	return responseChan
}

func (c *Client) SendRequest(moduleFrom string, moduleTo string, cmd interface{}) chan *Response {
	id := c.NextID()
	marshalledJSON, err := acbcjson.MarshalCmd(moduleFrom, moduleTo, id, cmd)
	if err != nil {
		return newFutureError(err)
	}

	// Generate the request and send it along with a channel to respond on.
	responseChan := make(chan *Response, 1)
	// re:MarshalCmd中的&Request 才是发给服务器的，下面的jReq只是为了保存 id 对应的responseChan，便于取结果
	jReq := &jsonRequest{
		id:             id,
		marshalledJSON: marshalledJSON,
		responseChan:   responseChan,
	}

	c.sendRequest(jReq)

	return responseChan
}

func (c *Client) SendTo(request *acbcjson.Request) []byte {
	fmt.Println("..............send to 函数")
	fmt.Printf("%+v\n", request)
	fmt.Println("..............request")
	//记录原本的id
	_ = request.ID
	id := c.NextID()

	// Generate the request and send it along with a channel to respond on.
	responseChan := make(chan *Response, 1)
	request.ID = id
	marshalledJSON, err := json.Marshal(request)
	if err != nil {
		jsonErr := &acbcjson.RPCError{
			Code: acbcjson.ErrRPCParse.Code,
			Message: fmt.Sprintf("Failed to marshal request: %v",
				err),
		}
		resp, err := acbcjson.MarshalResponse(request.ModuleFrom, request.ModuleTo, nil, nil, jsonErr)
		if err != nil {
			log.RpcsLog.Errorf("Failed to create reply: %v", err)
		}
		respBytes, _ := json.Marshal(resp)
		return respBytes
	}
	jReq := &jsonRequest{
		id:             id,
		marshalledJSON: marshalledJSON,
		responseChan:   responseChan,
	}
	fmt.Printf("%+v\n", jReq)
	fmt.Println("..............jReq")
	c.sendRequest(jReq)
	resp, err := ReceiveFuture(responseChan)
	if err != nil {
		jsonErr := &acbcjson.RPCError{
			Code: acbcjson.ErrRPCParse.Code,
			Message: fmt.Sprintf("Failed to  receiveFuture responseChan: %v",
				err),
		}
		resp, err := acbcjson.MarshalResponse(request.ModuleFrom, request.ModuleTo, nil, nil, jsonErr)
		if err != nil {
			log.RpcsLog.Errorf("Failed to create reply: %v", err)
		}
		respBytes, _ := json.Marshal(resp)
		return respBytes
	}
	fmt.Printf("%+v\n", resp)
	fmt.Println("..............resp")
	respBytes, err := json.Marshal(resp)
	if err != nil {
		fmt.Println(err.Error())
	}
	return respBytes
}

// ReceiveFuture receives from the passed futureResult channel to extract a
// reply or any errors.  The examined errors include an error in the
// futureResult and the error in the reply from the server.  This will block
// until the result is available on the passed channel.
func ReceiveFuture(f chan *Response) (*Response, error) {
	// Wait for a response on the returned channel.
	r := <-f
	return r, r.Error
}
