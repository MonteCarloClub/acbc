package main

import (
	"bytes"
	"container/list"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MonteCarloClub/acbc/acbcjson"
	"github.com/MonteCarloClub/acbc/acbcutil"
	"github.com/MonteCarloClub/acbc/wire"

	"github.com/btcsuite/websocket"
	"golang.org/x/crypto/ripemd160"
	"io"
	"sync"
	"time"
)

// wsNotificationManager is a connection and notification manager used for
// websockets.  It allows websocket clients to register for notifications they
// are interested in.  When an event happens elsewhere in the code such as
// transactions being added to the memory pool or block connects/disconnects,
// the notification manager is provided with the relevant details needed to
// figure out which websocket clients need to be notified based on what they
// have registered for and notifies them accordingly.  It is also used to keep
// track of all connected websocket clients.
type wsNotificationManager struct {
	// server is the RPC server the notification manager is associated with.
	server *rpcServer

	// queueNotification queues a notification for handling.
	queueNotification chan interface{}

	// notificationMsgs feeds notificationHandler with notifications
	// and client (un)registeration requests from a queue as well as
	// registeration and unregisteration requests from clients.
	notificationMsgs chan interface{}

	// Access channel for current number of connected clients.
	numClients chan int

	// Shutdown handling
	wg   sync.WaitGroup
	quit chan struct{}
}

const (
	// websocketSendBufferSize is the number of elements the send channel
	// can queue before blocking.  Note that this only applies to requests
	// handled directly in the websocket client input handler or the async
	// handler since notifications have their own queuing mechanism
	// independent of the send channel buffer.
	websocketSendBufferSize = 50
)

type semaphore chan struct{}

func makeSemaphore(n int) semaphore {
	return make(chan struct{}, n)
}

func (s semaphore) acquire() { s <- struct{}{} }
func (s semaphore) release() { <-s }

// wsResponse houses a message to send to a connected websocket client as
// well as a channel to reply on when the message is sent.
type wsResponse struct {
	msg      []byte
	doneChan chan bool
}

// wsClient provides an abstraction for handling a websocket client.  The
// overall data flow is split into 3 main goroutines, a possible 4th goroutine
// for long-running operations (only started if request is made), and a
// websocket manager which is used to allow things such as broadcasting
// requested notifications to all connected websocket clients.   Inbound
// messages are read via the inHandler goroutine and generally dispatched to
// their own handler.  However, certain potentially long-running operations such
// as rescans, are sent to the asyncHander goroutine and are limited to one at a
// time.  There are two outbound message types - one for responding to client
// requests and another for async notifications.  Responses to client requests
// use SendMessage which employs a buffered channel thereby limiting the number
// of outstanding requests that can be made.  Notifications are sent via
// QueueNotification which implements a queue via notificationQueueHandler to
// ensure sending notifications from other subsystems can't block.  Ultimately,
// all messages are sent via the outHandler.
type wsClient struct {
	sync.Mutex

	// server is the RPC server that is servicing the client.
	server *rpcServer

	// conn is the underlying websocket connection.
	conn *websocket.Conn

	// disconnected indicated whether or not the websocket client is
	// disconnected.
	disconnected bool

	// addr is the remote address of the client.
	addr string

	// authenticated specifies whether a client has been authenticated
	// and therefore is allowed to communicated over the websocket.
	authenticated bool

	// isAdmin specifies whether a client may change the state of the server;
	// false means its access is only to the limited set of RPC calls.
	isAdmin bool

	// sessionID is a random ID generated for each client when connected.
	// These IDs may be queried by a client using the session RPC.  A change
	// to the session ID indicates that the client reconnected.
	sessionID uint64

	// verboseTxUpdates specifies whether a client has requested verbose
	// information about all new transactions.
	verboseTxUpdates bool

	// addrRequests is a set of addresses the caller has requested to be
	// notified about.  It is maintained here so all requests can be removed
	// when a wallet disconnects.  Owned by the notification manager.
	addrRequests map[string]struct{}

	// spentRequests is a set of unspent Outpoints a wallet has requested
	// notifications for when they are spent by a processed transaction.
	// Owned by the notification manager.
	spentRequests map[wire.OutPoint]struct{}

	// filterData is the new generation transaction filter backported from
	// github.com/decred/dcrd for the new backported `loadtxfilter` and
	// `rescanblocks` methods.
	filterData *wsClientFilter

	// Networking infrastructure.
	serviceRequestSem semaphore
	ntfnChan          chan []byte
	sendChan          chan wsResponse
	quit              chan struct{}
	wg                sync.WaitGroup
}

// wsClientFilter tracks relevant addresses for each websocket client for
// the `rescanblocks` extension. It is modified by the `loadtxfilter` command.
//
// NOTE: This extension was ported from github.com/decred/dcrd
type wsClientFilter struct {
	mu sync.Mutex

	// Implemented fast paths for address lookup.
	pubKeyHashes        map[[ripemd160.Size]byte]struct{}
	scriptHashes        map[[ripemd160.Size]byte]struct{}
	compressedPubKeys   map[[33]byte]struct{}
	uncompressedPubKeys map[[65]byte]struct{}

	// A fallback address lookup map in case a fast path doesn't exist.
	// Only exists for completeness.  If using this shows up in a profile,
	// there's a good chance a fast path should be added.
	otherAddresses map[string]struct{}

	// Outpoints of unspent outputs.
	unspent map[wire.OutPoint]struct{}
}

// Notification types
type notificationBlockConnected acbcutil.Block
type notificationBlockDisconnected acbcutil.Block
type notificationTxAcceptedByMempool struct {
	isNew bool
	tx    *acbcutil.Tx
}

// Notification control requests
type notificationRegisterClient wsClient
type notificationUnregisterClient wsClient
type notificationRegisterBlocks wsClient
type notificationUnregisterBlocks wsClient
type notificationRegisterNewMempoolTxs wsClient
type notificationUnregisterNewMempoolTxs wsClient
type notificationRegisterSpent struct {
	wsc *wsClient
	ops []*wire.OutPoint
}
type notificationUnregisterSpent struct {
	wsc *wsClient
	op  *wire.OutPoint
}
type notificationRegisterAddr struct {
	wsc   *wsClient
	addrs []string
}
type notificationUnregisterAddr struct {
	wsc  *wsClient
	addr string
}

// wsCommandHandler describes a callback function used to handle a specific
// command.
type wsCommandHandler func(*wsClient, interface{}) (interface{}, error)

// wsHandlers maps RPC command strings to appropriate websocket handler
// functions.  This is set by init because help references wsHandlers and thus
// causes a dependency loop.
var wsHandlers map[string]wsCommandHandler
var wsHandlersBeforeInit = map[string]wsCommandHandler{
	/*
		"loadtxfilter":              handleLoadTxFilter,
		"help":                      handleWebsocketHelp,
		"notifyblocks":              handleNotifyBlocks,
		"notifynewtransactions":     handleNotifyNewTransactions,
		"notifyreceived":            handleNotifyReceived,
		"notifyspent":               handleNotifySpent,
		"session":                   handleSession,
		"stopnotifyblocks":          handleStopNotifyBlocks,
		"stopnotifynewtransactions": handleStopNotifyNewTransactions,
		"stopnotifyspent":           handleStopNotifySpent,
		"stopnotifyreceived":        handleStopNotifyReceived,
		"rescan":                    handleRescan,
		"rescanblocks":              handleRescanBlocks,

	*/
}

// timeZeroVal is simply the zero value for a time.Time and is used to avoid
// creating multiple instances.
var timeZeroVal time.Time

// newWsNotificationManager returns a new notification manager ready for use.
// See wsNotificationManager for more details.
func newWsNotificationManager(server *rpcServer) *wsNotificationManager {
	return &wsNotificationManager{
		server:            server,
		queueNotification: make(chan interface{}),
		notificationMsgs:  make(chan interface{}),
		numClients:        make(chan int),
		quit:              make(chan struct{}),
	}
}

// NumClients returns the number of clients actively being served.
func (m *wsNotificationManager) NumClients() (n int) {
	select {
	case n = <-m.numClients:
	case <-m.quit: // Use default n (0) if server has shut down.
	}
	return
}

// AddClient adds the passed websocket client to the notification manager.
func (m *wsNotificationManager) AddClient(wsc *wsClient) {
	m.queueNotification <- (*notificationRegisterClient)(wsc)
}

// RemoveClient removes the passed websocket client and all notifications
// registered for it.
func (m *wsNotificationManager) RemoveClient(wsc *wsClient) {
	select {
	case m.queueNotification <- (*notificationUnregisterClient)(wsc):
	case <-m.quit:
	}
}

// Start starts the goroutines required for the manager to queue and process
// websocket client notifications.
func (m *wsNotificationManager) Start() {
	m.wg.Add(2)
	go m.queueHandler()
	go m.notificationHandler()
}

// WaitForShutdown blocks until all notification manager goroutines have
// finished.
func (m *wsNotificationManager) WaitForShutdown() {
	m.wg.Wait()
}

// newWebsocketClient returns a new websocket client given the notification
// manager, websocket connection, remote address, and whether or not the client
// has already been authenticated (via HTTP Basic access authentication).  The
// returned client is ready to start.  Once started, the client will process
// incoming and outgoing messages in separate goroutines complete with queuing
// and asynchrous handling for long-running operations.
func newWebsocketClient(server *rpcServer, conn *websocket.Conn,
	remoteAddr string, authenticated bool, isAdmin bool) (*wsClient, error) {

	sessionID, err := wire.RandomUint64()
	if err != nil {
		return nil, err
	}

	client := &wsClient{
		conn:              conn,
		addr:              remoteAddr,
		authenticated:     authenticated,
		isAdmin:           isAdmin,
		sessionID:         sessionID,
		server:            server,
		addrRequests:      make(map[string]struct{}),
		spentRequests:     make(map[wire.OutPoint]struct{}),
		serviceRequestSem: makeSemaphore(cfg.RPCMaxConcurrentReqs),
		ntfnChan:          make(chan []byte, 1), // nonblocking sync
		sendChan:          make(chan wsResponse, websocketSendBufferSize),
		quit:              make(chan struct{}),
	}
	return client, nil
}

// serviceRequest services a parsed RPC request by looking up and executing the
// appropriate RPC handler.  The response is marshalled and sent to the
// websocket client.
func (c *wsClient) serviceRequest(r *parsedRPCCmd) {
	var (
		result interface{}
		err    error
	)

	// Lookup the websocket extension for the command and if it doesn't
	// exist fallback to handling the command as a standard command.
	wsHandler, ok := wsHandlers[r.method]
	if ok {
		result, err = wsHandler(c, r.cmd)
	} else {
		result, err = c.server.standardCmdResult(r, nil)
	}
	reply, err := createMarshalledReply(r.jsonrpc, r.id, result, err)
	if err != nil {
		rpcsLog.Errorf("Failed to marshal reply for <%s> "+
			"command: %v", r.method, err)
		return
	}
	c.SendMessage(reply, nil)
}

// inHandler handles all incoming messages for the websocket connection.  It
// must be run as a goroutine.
func (c *wsClient) inHandler() {
out:
	for {
		// Break out of the loop once the quit channel has been closed.
		// Use a non-blocking select here so we fall through otherwise.
		select {
		case <-c.quit:
			break out
		default:
		}

		_, msg, err := c.conn.ReadMessage()
		if err != nil {
			// Log the error if it's not due to disconnecting.
			if err != io.EOF {
				rpcsLog.Errorf("Websocket receive error from "+
					"%s: %v", c.addr, err)
			}
			break out
		}

		var batchedRequest bool

		// Determine request type
		if bytes.HasPrefix(msg, batchedRequestPrefix) {
			batchedRequest = true
		}

		if !batchedRequest {
			var req acbcjson.Request
			var reply json.RawMessage
			err = json.Unmarshal(msg, &req)
			if err != nil {
				// only process requests from authenticated clients
				if !c.authenticated {
					break out
				}

				jsonErr := &acbcjson.RPCError{
					Code:    acbcjson.ErrRPCParse.Code,
					Message: "Failed to parse request: " + err.Error(),
				}
				reply, err = createMarshalledReply(acbcjson.RpcVersion1, nil, nil, jsonErr)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal reply: %v", err)
					continue
				}
				c.SendMessage(reply, nil)
				continue
			}

			if req.Method == "" || req.Params == nil {
				jsonErr := &acbcjson.RPCError{
					Code:    acbcjson.ErrRPCInvalidRequest.Code,
					Message: "Invalid request: malformed",
				}
				reply, err := createMarshalledReply(req.Jsonrpc, req.ID, nil, jsonErr)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal reply: %v", err)
					continue
				}
				c.SendMessage(reply, nil)
				continue
			}

			// Valid requests with no ID (notifications) must not have a response
			// per the JSON-RPC spec.
			if req.ID == nil {
				if !c.authenticated {
					break out
				}
				continue
			}

			cmd := parseCmd(&req)
			if cmd.err != nil {
				// Only process requests from authenticated clients
				if !c.authenticated {
					break out
				}

				reply, err = createMarshalledReply(cmd.jsonrpc, cmd.id, nil, cmd.err)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal reply: %v", err)
					continue
				}
				c.SendMessage(reply, nil)
				continue
			}

			rpcsLog.Debugf("Received command <%s> from %s", cmd.method, c.addr)

			// Check auth.  The client is immediately disconnected if the
			// first request of an unauthentiated websocket client is not
			// the authenticate request, an authenticate request is received
			// when the client is already authenticated, or incorrect
			// authentication credentials are provided in the request.
			switch authCmd, ok := cmd.cmd.(*acbcjson.AuthenticateCmd); {
			case c.authenticated && ok:
				rpcsLog.Warnf("Websocket client %s is already authenticated",
					c.addr)
				break out
			case !c.authenticated && !ok:
				rpcsLog.Warnf("Unauthenticated websocket message " +
					"received")
				break out
			case !c.authenticated:
				// Check credentials.
				login := authCmd.Username + ":" + authCmd.Passphrase
				auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(login))
				authSha := sha256.Sum256([]byte(auth))
				cmp := subtle.ConstantTimeCompare(authSha[:], c.server.authsha[:])
				limitcmp := subtle.ConstantTimeCompare(authSha[:], c.server.limitauthsha[:])
				if cmp != 1 && limitcmp != 1 {
					rpcsLog.Warnf("Auth failure.")
					break out
				}
				c.authenticated = true
				c.isAdmin = cmp == 1

				// Marshal and send response.
				reply, err = createMarshalledReply(cmd.jsonrpc, cmd.id, nil, nil)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal authenticate reply: "+
						"%v", err.Error())
					continue
				}
				c.SendMessage(reply, nil)
				continue
			}

			// Check if the client is using limited RPC credentials and
			// error when not authorized to call the supplied RPC.
			if !c.isAdmin {
				if _, ok := rpcLimited[req.Method]; !ok {
					jsonErr := &acbcjson.RPCError{
						Code:    acbcjson.ErrRPCInvalidParams.Code,
						Message: "limited user not authorized for this method",
					}
					// Marshal and send response.
					reply, err = createMarshalledReply("", req.ID, nil, jsonErr)
					if err != nil {
						rpcsLog.Errorf("Failed to marshal parse failure "+
							"reply: %v", err)
						continue
					}
					c.SendMessage(reply, nil)
					continue
				}
			}

			// Asynchronously handle the request.  A semaphore is used to
			// limit the number of concurrent requests currently being
			// serviced.  If the semaphore can not be acquired, simply wait
			// until a request finished before reading the next RPC request
			// from the websocket client.
			//
			// This could be a little fancier by timing out and erroring
			// when it takes too long to service the request, but if that is
			// done, the read of the next request should not be blocked by
			// this semaphore, otherwise the next request will be read and
			// will probably sit here for another few seconds before timing
			// out as well.  This will cause the total timeout duration for
			// later requests to be much longer than the check here would
			// imply.
			//
			// If a timeout is added, the semaphore acquiring should be
			// moved inside of the new goroutine with a select statement
			// that also reads a time.After channel.  This will unblock the
			// read of the next request from the websocket client and allow
			// many requests to be waited on concurrently.
			c.serviceRequestSem.acquire()
			go func() {
				c.serviceRequest(cmd)
				c.serviceRequestSem.release()
			}()
		}

		// Process a batched request
		if batchedRequest {
			var batchedRequests []interface{}
			var results []json.RawMessage
			var batchSize int
			var reply json.RawMessage
			c.serviceRequestSem.acquire()
			err = json.Unmarshal(msg, &batchedRequests)
			if err != nil {
				// Only process requests from authenticated clients
				if !c.authenticated {
					break out
				}

				jsonErr := &acbcjson.RPCError{
					Code: acbcjson.ErrRPCParse.Code,
					Message: fmt.Sprintf("Failed to parse request: %v",
						err),
				}
				reply, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
				if err != nil {
					rpcsLog.Errorf("Failed to create reply: %v", err)
				}

				if reply != nil {
					results = append(results, reply)
				}
			}

			if err == nil {
				// Response with an empty batch error if the batch size is zero
				if len(batchedRequests) == 0 {
					if !c.authenticated {
						break out
					}

					jsonErr := &acbcjson.RPCError{
						Code:    acbcjson.ErrRPCInvalidRequest.Code,
						Message: "Invalid request: empty batch",
					}
					reply, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
					if err != nil {
						rpcsLog.Errorf("Failed to marshal reply: %v", err)
					}

					if reply != nil {
						results = append(results, reply)
					}
				}

				// Process each batch entry individually
				if len(batchedRequests) > 0 {
					batchSize = len(batchedRequests)
					for _, entry := range batchedRequests {
						var reqBytes []byte
						reqBytes, err = json.Marshal(entry)
						if err != nil {
							// Only process requests from authenticated clients
							if !c.authenticated {
								break out
							}

							jsonErr := &acbcjson.RPCError{
								Code: acbcjson.ErrRPCInvalidRequest.Code,
								Message: fmt.Sprintf("Invalid request: %v",
									err),
							}
							reply, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
							if err != nil {
								rpcsLog.Errorf("Failed to create reply: %v", err)
								continue
							}

							if reply != nil {
								results = append(results, reply)
							}
							continue
						}

						var req acbcjson.Request
						err := json.Unmarshal(reqBytes, &req)
						if err != nil {
							// Only process requests from authenticated clients
							if !c.authenticated {
								break out
							}

							jsonErr := &acbcjson.RPCError{
								Code: acbcjson.ErrRPCInvalidRequest.Code,
								Message: fmt.Sprintf("Invalid request: %v",
									err),
							}
							reply, err = acbcjson.MarshalResponse(acbcjson.RpcVersion2, nil, nil, jsonErr)
							if err != nil {
								rpcsLog.Errorf("Failed to create reply: %v", err)
								continue
							}

							if reply != nil {
								results = append(results, reply)
							}
							continue
						}

						if req.Method == "" || req.Params == nil {
							jsonErr := &acbcjson.RPCError{
								Code:    acbcjson.ErrRPCInvalidRequest.Code,
								Message: "Invalid request: malformed",
							}
							reply, err := createMarshalledReply(req.Jsonrpc, req.ID, nil, jsonErr)
							if err != nil {
								rpcsLog.Errorf("Failed to marshal reply: %v", err)
								continue
							}

							if reply != nil {
								results = append(results, reply)
							}
							continue
						}

						// Valid requests with no ID (notifications) must not have a response
						// per the JSON-RPC spec.
						if req.ID == nil {
							if !c.authenticated {
								break out
							}
							continue
						}

						cmd := parseCmd(&req)
						if cmd.err != nil {
							// Only process requests from authenticated clients
							if !c.authenticated {
								break out
							}

							reply, err = createMarshalledReply(cmd.jsonrpc, cmd.id, nil, cmd.err)
							if err != nil {
								rpcsLog.Errorf("Failed to marshal reply: %v", err)
								continue
							}

							if reply != nil {
								results = append(results, reply)
							}
							continue
						}

						rpcsLog.Debugf("Received command <%s> from %s", cmd.method, c.addr)

						// Check auth.  The client is immediately disconnected if the
						// first request of an unauthentiated websocket client is not
						// the authenticate request, an authenticate request is received
						// when the client is already authenticated, or incorrect
						// authentication credentials are provided in the request.
						switch authCmd, ok := cmd.cmd.(*acbcjson.AuthenticateCmd); {
						case c.authenticated && ok:
							rpcsLog.Warnf("Websocket client %s is already authenticated",
								c.addr)
							break out
						case !c.authenticated && !ok:
							rpcsLog.Warnf("Unauthenticated websocket message " +
								"received")
							break out
						case !c.authenticated:
							// Check credentials.
							login := authCmd.Username + ":" + authCmd.Passphrase
							auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(login))
							authSha := sha256.Sum256([]byte(auth))
							cmp := subtle.ConstantTimeCompare(authSha[:], c.server.authsha[:])
							limitcmp := subtle.ConstantTimeCompare(authSha[:], c.server.limitauthsha[:])
							if cmp != 1 && limitcmp != 1 {
								rpcsLog.Warnf("Auth failure.")
								break out
							}

							c.authenticated = true
							c.isAdmin = cmp == 1

							// Marshal and send response.
							reply, err = createMarshalledReply(cmd.jsonrpc, cmd.id, nil, nil)
							if err != nil {
								rpcsLog.Errorf("Failed to marshal authenticate reply: "+
									"%v", err.Error())
								continue
							}

							if reply != nil {
								results = append(results, reply)
							}
							continue
						}

						// Check if the client is using limited RPC credentials and
						// error when not authorized to call the supplied RPC.
						if !c.isAdmin {
							if _, ok := rpcLimited[req.Method]; !ok {
								jsonErr := &acbcjson.RPCError{
									Code:    acbcjson.ErrRPCInvalidParams.Code,
									Message: "limited user not authorized for this method",
								}
								// Marshal and send response.
								reply, err = createMarshalledReply(req.Jsonrpc, req.ID, nil, jsonErr)
								if err != nil {
									rpcsLog.Errorf("Failed to marshal parse failure "+
										"reply: %v", err)
									continue
								}

								if reply != nil {
									results = append(results, reply)
								}
								continue
							}
						}

						// Lookup the websocket extension for the command, if it doesn't
						// exist fallback to handling the command as a standard command.
						var resp interface{}
						wsHandler, ok := wsHandlers[cmd.method]
						if ok {
							resp, err = wsHandler(c, cmd.cmd)
						} else {
							resp, err = c.server.standardCmdResult(cmd, nil)
						}

						// Marshal request output.
						reply, err := createMarshalledReply(cmd.jsonrpc, cmd.id, resp, err)
						if err != nil {
							rpcsLog.Errorf("Failed to marshal reply for <%s> "+
								"command: %v", cmd.method, err)
							return
						}

						if reply != nil {
							results = append(results, reply)
						}
					}
				}
			}

			// generate reply
			var payload = []byte{}
			if batchedRequest && batchSize > 0 {
				if len(results) > 0 {
					// Form the batched response json
					var buffer bytes.Buffer
					buffer.WriteByte('[')
					for idx, marshalledReply := range results {
						if idx == len(results)-1 {
							buffer.Write(marshalledReply)
							buffer.WriteByte(']')
							break
						}
						buffer.Write(marshalledReply)
						buffer.WriteByte(',')
					}
					payload = buffer.Bytes()
				}
			}

			if !batchedRequest || batchSize == 0 {
				// Respond with the first results entry for single requests
				if len(results) > 0 {
					payload = results[0]
				}
			}

			c.SendMessage(payload, nil)
			c.serviceRequestSem.release()
		}
	}

	// Ensure the connection is closed.
	c.Disconnect()
	c.wg.Done()
	rpcsLog.Tracef("Websocket client input handler done for %s", c.addr)
}

// notificationQueueHandler handles the queuing of outgoing notifications for
// the websocket client.  This runs as a muxer for various sources of input to
// ensure that queuing up notifications to be sent will not block.  Otherwise,
// slow clients could bog down the other systems (such as the mempool or block
// manager) which are queuing the data.  The data is passed on to outHandler to
// actually be written.  It must be run as a goroutine.
func (c *wsClient) notificationQueueHandler() {
	ntfnSentChan := make(chan bool, 1) // nonblocking sync

	// pendingNtfns is used as a queue for notifications that are ready to
	// be sent once there are no outstanding notifications currently being
	// sent.  The waiting flag is used over simply checking for items in the
	// pending list to ensure cleanup knows what has and hasn't been sent
	// to the outHandler.  Currently no special cleanup is needed, however
	// if something like a done channel is added to notifications in the
	// future, not knowing what has and hasn't been sent to the outHandler
	// (and thus who should respond to the done channel) would be
	// problematic without using this approach.
	pendingNtfns := list.New()
	waiting := false
out:
	for {
		select {
		// This channel is notified when a message is being queued to
		// be sent across the network socket.  It will either send the
		// message immediately if a send is not already in progress, or
		// queue the message to be sent once the other pending messages
		// are sent.
		case msg := <-c.ntfnChan:
			if !waiting {
				c.SendMessage(msg, ntfnSentChan)
			} else {
				pendingNtfns.PushBack(msg)
			}
			waiting = true

		// This channel is notified when a notification has been sent
		// across the network socket.
		case <-ntfnSentChan:
			// No longer waiting if there are no more messages in
			// the pending messages queue.
			next := pendingNtfns.Front()
			if next == nil {
				waiting = false
				continue
			}

			// Notify the outHandler about the next item to
			// asynchronously send.
			msg := pendingNtfns.Remove(next).([]byte)
			c.SendMessage(msg, ntfnSentChan)

		case <-c.quit:
			break out
		}
	}

	// Drain any wait channels before exiting so nothing is left waiting
	// around to send.
cleanup:
	for {
		select {
		case <-c.ntfnChan:
		case <-ntfnSentChan:
		default:
			break cleanup
		}
	}
	c.wg.Done()
	rpcsLog.Tracef("Websocket client notification queue handler done "+
		"for %s", c.addr)
}

// outHandler handles all outgoing messages for the websocket connection.  It
// must be run as a goroutine.  It uses a buffered channel to serialize output
// messages while allowing the sender to continue running asynchronously.  It
// must be run as a goroutine.
func (c *wsClient) outHandler() {
out:
	for {
		// Send any messages ready for send until the quit channel is
		// closed.
		select {
		case r := <-c.sendChan:
			err := c.conn.WriteMessage(websocket.TextMessage, r.msg)
			if err != nil {
				c.Disconnect()
				break out
			}
			if r.doneChan != nil {
				r.doneChan <- true
			}

		case <-c.quit:
			break out
		}
	}

	// Drain any wait channels before exiting so nothing is left waiting
	// around to send.
cleanup:
	for {
		select {
		case r := <-c.sendChan:
			if r.doneChan != nil {
				r.doneChan <- false
			}
		default:
			break cleanup
		}
	}
	c.wg.Done()
	rpcsLog.Tracef("Websocket client output handler done for %s", c.addr)
}

// Disconnected returns whether or not the websocket client is disconnected.
func (c *wsClient) Disconnected() bool {
	c.Lock()
	isDisconnected := c.disconnected
	c.Unlock()

	return isDisconnected
}

// SendMessage sends the passed json to the websocket client.  It is backed
// by a buffered channel, so it will not block until the send channel is full.
// Note however that QueueNotification must be used for sending async
// notifications instead of the this function.  This approach allows a limit to
// the number of outstanding requests a client can make without preventing or
// blocking on async notifications.
func (c *wsClient) SendMessage(marshalledJSON []byte, doneChan chan bool) {
	// Don't send the message if disconnected.
	if c.Disconnected() {
		if doneChan != nil {
			doneChan <- false
		}
		return
	}

	c.sendChan <- wsResponse{msg: marshalledJSON, doneChan: doneChan}
}

// Disconnect disconnects the websocket client.
func (c *wsClient) Disconnect() {
	c.Lock()
	defer c.Unlock()

	// Nothing to do if already disconnected.
	if c.disconnected {
		return
	}

	rpcsLog.Tracef("Disconnecting websocket client %s", c.addr)
	close(c.quit)
	c.conn.Close()
	c.disconnected = true
}

// Start begins processing input and output messages.
func (c *wsClient) Start() {
	rpcsLog.Tracef("Starting websocket client %s", c.addr)

	// Start processing input and output.
	c.wg.Add(3)
	go c.inHandler()
	go c.notificationQueueHandler()
	go c.outHandler()
}

// WaitForShutdown blocks until the websocket client goroutines are stopped
// and the connection is closed.
func (c *wsClient) WaitForShutdown() {
	c.wg.Wait()
}

// WebsocketHandler handles a new websocket client by creating a new wsClient,
// starting it, and blocking until the connection closes.  Since it blocks, it
// must be run in a separate goroutine.  It should be invoked from the websocket
// server handler which runs each new connection in a new goroutine thereby
// satisfying the requirement.
func (s *rpcServer) WebsocketHandler(conn *websocket.Conn, remoteAddr string,
	authenticated bool, isAdmin bool) {

	// Clear the read deadline that was set before the websocket hijacked
	// the connection.
	conn.SetReadDeadline(timeZeroVal)

	// Limit max number of websocket clients.
	rpcsLog.Infof("New websocket client %s", remoteAddr)
	if s.ntfnMgr.NumClients()+1 > cfg.RPCMaxWebsockets {
		rpcsLog.Infof("Max websocket clients exceeded [%d] - "+
			"disconnecting client %s", cfg.RPCMaxWebsockets,
			remoteAddr)
		conn.Close()
		return
	}

	// Create a new websocket client to handle the new websocket connection
	// and wait for it to shutdown.  Once it has shutdown (and hence
	// disconnected), remove it and any notifications it registered for.
	client, err := newWebsocketClient(s, conn, remoteAddr, authenticated, isAdmin)
	if err != nil {
		rpcsLog.Errorf("Failed to serve client %s: %v", remoteAddr, err)
		conn.Close()
		return
	}
	s.ntfnMgr.AddClient(client)
	client.Start()
	client.WaitForShutdown()
	s.ntfnMgr.RemoveClient(client)
	rpcsLog.Infof("Disconnected websocket client %s", remoteAddr)
}

// queueHandler manages a queue of empty interfaces, reading from in and
// sending the oldest unsent to out.  This handler stops when either of the
// in or quit channels are closed, and closes out before returning, without
// waiting to send any variables still remaining in the queue.
func queueHandler(in <-chan interface{}, out chan<- interface{}, quit <-chan struct{}) {
	var q []interface{}
	var dequeue chan<- interface{}
	skipQueue := out
	var next interface{}
out:
	for {
		select {
		case n, ok := <-in:
			if !ok {
				// Sender closed input channel.
				break out
			}

			// Either send to out immediately if skipQueue is
			// non-nil (queue is empty) and reader is ready,
			// or append to the queue and send later.
			select {
			case skipQueue <- n:
			default:
				q = append(q, n)
				dequeue = out
				skipQueue = nil
				next = q[0]
			}

		case dequeue <- next:
			copy(q, q[1:])
			q[len(q)-1] = nil // avoid leak
			q = q[:len(q)-1]
			if len(q) == 0 {
				dequeue = nil
				skipQueue = out
			} else {
				next = q[0]
			}

		case <-quit:
			break out
		}
	}
	close(out)
}

// queueHandler maintains a queue of notifications and notification handler
// control messages.
func (m *wsNotificationManager) queueHandler() {
	queueHandler(m.queueNotification, m.notificationMsgs, m.quit)
	m.wg.Done()
}

// notifyForTx examines the inputs and outputs of the passed transaction,
// notifying websocket clients of outputs spending to a watched address
// and inputs spending a watched outpoint.
func (m *wsNotificationManager) notifyForTx(ops map[wire.OutPoint]map[chan struct{}]*wsClient,
	addrs map[string]map[chan struct{}]*wsClient, tx *acbcutil.Tx, block *acbcutil.Block) {

	if len(ops) != 0 {
		m.notifyForTxIns(ops, tx, block)
	}
	if len(addrs) != 0 {
		m.notifyForTxOuts(ops, addrs, tx, block)
	}
}

// txHexString returns the serialized transaction encoded in hexadecimal.
func txHexString(tx *wire.MsgTx) string {
	buf := bytes.NewBuffer(make([]byte, 0, tx.SerializeSize()))
	// Ignore Serialize's error, as writing to a bytes.buffer cannot fail.
	tx.Serialize(buf)
	return hex.EncodeToString(buf.Bytes())
}

// blockDetails creates a BlockDetails struct to include in btcws notifications
// from a block and a transaction's block index.
func blockDetails(block *acbcutil.Block, txIndex int) *acbcjson.BlockDetails {
	if block == nil {
		return nil
	}
	return &acbcjson.BlockDetails{
		Height: block.Height(),
		Hash:   block.Hash().String(),
		Index:  txIndex,
		Time:   block.MsgBlock().Header.Timestamp.Unix(),
	}
}

// newRedeemingTxNotification returns a new marshalled redeemingtx notification
// with the passed parameters.
func newRedeemingTxNotification(txHex string, index int, block *acbcutil.Block) ([]byte, error) {
	// Create and marshal the notification.
	ntfn := acbcjson.NewRedeemingTxNtfn(txHex, blockDetails(block, index))
	return acbcjson.MarshalCmd(acbcjson.RpcVersion1, nil, ntfn)
}

// removeSpentRequest modifies a map of watched outpoints to remove the
// websocket client wsc from the set of clients to be notified when a
// watched outpoint is spent.  If wsc is the last client, the outpoint
// key is removed from the map.
func (*wsNotificationManager) removeSpentRequest(ops map[wire.OutPoint]map[chan struct{}]*wsClient,
	wsc *wsClient, op *wire.OutPoint) {

	// Remove the request tracking from the client.
	delete(wsc.spentRequests, *op)

	// Remove the client from the list to notify.
	notifyMap, ok := ops[*op]
	if !ok {
		rpcsLog.Warnf("Attempt to remove nonexistent spent request "+
			"for websocket client %s", wsc.addr)
		return
	}
	delete(notifyMap, wsc.quit)

	// Remove the map entry altogether if there are
	// no more clients interested in it.
	if len(notifyMap) == 0 {
		delete(ops, *op)
	}
}

// ErrClientQuit describes the error where a client send is not processed due
// to the client having already been disconnected or dropped.
var ErrClientQuit = errors.New("client quit")

// QueueNotification queues the passed notification to be sent to the websocket
// client.  This function, as the name implies, is only intended for
// notifications since it has additional logic to prevent other subsystems, such
// as the memory pool and block manager, from blocking even when the send
// channel is full.
//
// If the client is in the process of shutting down, this function returns
// ErrClientQuit.  This is intended to be checked by long-running notification
// handlers to stop processing if there is no more work needed to be done.
func (c *wsClient) QueueNotification(marshalledJSON []byte) error {
	// Don't queue the message if disconnected.
	if c.Disconnected() {
		return ErrClientQuit
	}

	c.ntfnChan <- marshalledJSON
	return nil
}

// notifyForTxIns examines the inputs of the passed transaction and sends
// interested websocket clients a redeemingtx notification if any inputs
// spend a watched output.  If block is non-nil, any matching spent
// requests are removed.
func (m *wsNotificationManager) notifyForTxIns(ops map[wire.OutPoint]map[chan struct{}]*wsClient,
	tx *acbcutil.Tx, block *acbcutil.Block) {

	// Nothing to do if nobody is watching outpoints.
	if len(ops) == 0 {
		return
	}

	txHex := ""
	wscNotified := make(map[chan struct{}]struct{})
	for _, txIn := range tx.MsgTx().TxIn {
		prevOut := &txIn.PreviousOutPoint
		if cmap, ok := ops[*prevOut]; ok {
			if txHex == "" {
				txHex = txHexString(tx.MsgTx())
			}
			marshalledJSON, err := newRedeemingTxNotification(txHex, tx.Index(), block)
			if err != nil {
				rpcsLog.Warnf("Failed to marshal redeemingtx notification: %v", err)
				continue
			}
			for wscQuit, wsc := range cmap {
				if block != nil {
					m.removeSpentRequest(ops, wsc, prevOut)
				}

				if _, ok := wscNotified[wscQuit]; !ok {
					wscNotified[wscQuit] = struct{}{}
					wsc.QueueNotification(marshalledJSON)
				}
			}
		}
	}
}

// notifyForTxOuts examines each transaction output, notifying interested
// websocket clients of the transaction if an output spends to a watched
// address.  A spent notification request is automatically registered for
// the client for each matching output.
func (m *wsNotificationManager) notifyForTxOuts(ops map[wire.OutPoint]map[chan struct{}]*wsClient,
	addrs map[string]map[chan struct{}]*wsClient, tx *acbcutil.Tx, block *acbcutil.Block) {
	/*

		// Nothing to do if nobody is listening for address notifications.
		if len(addrs) == 0 {
			return
		}

		txHex := ""
		wscNotified := make(map[chan struct{}]struct{})
		for i, txOut := range tx.MsgTx().TxOut {
			_, txAddrs, _, err := txscript.ExtractPkScriptAddrs(
				txOut.PkScript, m.server.cfg.ChainParams)
			if err != nil {
				continue
			}

			for _, txAddr := range txAddrs {
				cmap, ok := addrs[txAddr.EncodeAddress()]
				if !ok {
					continue
				}

				if txHex == "" {
					txHex = txHexString(tx.MsgTx())
				}
				ntfn := acbcjson.NewRecvTxNtfn(txHex, blockDetails(block,
					tx.Index()))

				marshalledJSON, err := acbcjson.MarshalCmd(acbcjson.RpcVersion1, nil, ntfn)
				if err != nil {
					rpcsLog.Errorf("Failed to marshal processedtx notification: %v", err)
					continue
				}

				op := []*wire.OutPoint{wire.NewOutPoint(tx.Hash(), uint32(i))}
				for wscQuit, wsc := range cmap {
					m.addSpentRequests(ops, wsc, op)

					if _, ok := wscNotified[wscQuit]; !ok {
						wscNotified[wscQuit] = struct{}{}
						wsc.QueueNotification(marshalledJSON)
					}
				}
			}
		}

	*/
}

// notificationHandler reads notifications and control messages from the queue
// handler and processes one at a time.
func (m *wsNotificationManager) notificationHandler() {
	// clients is a map of all currently connected websocket clients.
	clients := make(map[chan struct{}]*wsClient)

	// Maps used to hold lists of websocket clients to be notified on
	// certain events.  Each websocket client also keeps maps for the events
	// which have multiple triggers to make removal from these lists on
	// connection close less horrendously expensive.
	//
	// Where possible, the quit channel is used as the unique id for a client
	// since it is quite a bit more efficient than using the entire struct.
	blockNotifications := make(map[chan struct{}]*wsClient)
	txNotifications := make(map[chan struct{}]*wsClient)
	//watchedOutPoints := make(map[wire.OutPoint]map[chan struct{}]*wsClient)
	//watchedAddrs := make(map[string]map[chan struct{}]*wsClient)

out:
	for {
		select {
		case n, ok := <-m.notificationMsgs:
			if !ok {
				// queueHandler quit.
				break out
			}
			switch n := n.(type) {
			/*
				case *notificationBlockConnected:
					block := (*acbcutil.Block)(n)

					// Skip iterating through all txs if no
					// tx notification requests exist.
					if len(watchedOutPoints) != 0 || len(watchedAddrs) != 0 {
						for _, tx := range block.Transactions() {
							m.notifyForTx(watchedOutPoints,
								watchedAddrs, tx, block)
						}
					}

					if len(blockNotifications) != 0 {
						m.notifyBlockConnected(blockNotifications,
							block)
						m.notifyFilteredBlockConnected(blockNotifications,
							block)
					}

				case *notificationBlockDisconnected:
					block := (*acbcutil.Block)(n)

					if len(blockNotifications) != 0 {
						m.notifyBlockDisconnected(blockNotifications,
							block)
						m.notifyFilteredBlockDisconnected(blockNotifications,
							block)
					}

				case *notificationTxAcceptedByMempool:
					if n.isNew && len(txNotifications) != 0 {
						m.notifyForNewTx(txNotifications, n.tx)
					}
					m.notifyForTx(watchedOutPoints, watchedAddrs, n.tx, nil)
					m.notifyRelevantTxAccepted(n.tx, clients)
			*/
			case *notificationRegisterBlocks:
				wsc := (*wsClient)(n)
				blockNotifications[wsc.quit] = wsc

			case *notificationUnregisterBlocks:
				wsc := (*wsClient)(n)
				delete(blockNotifications, wsc.quit)

			case *notificationRegisterClient:
				wsc := (*wsClient)(n)
				clients[wsc.quit] = wsc
				/*
					case *notificationUnregisterClient:
						wsc := (*wsClient)(n)
						// Remove any requests made by the client as well as
						// the client itself.
						delete(blockNotifications, wsc.quit)
						delete(txNotifications, wsc.quit)
						for k := range wsc.spentRequests {
							op := k
							m.removeSpentRequest(watchedOutPoints, wsc, &op)
						}
						for addr := range wsc.addrRequests {
							m.removeAddrRequest(watchedAddrs, wsc, addr)
						}
						delete(clients, wsc.quit)

					case *notificationRegisterSpent:
						m.addSpentRequests(watchedOutPoints, n.wsc, n.ops)

					case *notificationUnregisterSpent:
						m.removeSpentRequest(watchedOutPoints, n.wsc, n.op)

					case *notificationRegisterAddr:
						m.addAddrRequests(watchedAddrs, n.wsc, n.addrs)

					case *notificationUnregisterAddr:
						m.removeAddrRequest(watchedAddrs, n.wsc, n.addr)
				*/
			case *notificationRegisterNewMempoolTxs:
				wsc := (*wsClient)(n)
				txNotifications[wsc.quit] = wsc

			case *notificationUnregisterNewMempoolTxs:
				wsc := (*wsClient)(n)
				delete(txNotifications, wsc.quit)

			default:
				rpcsLog.Warn("Unhandled notification type")
			}

		case m.numClients <- len(clients):

		case <-m.quit:
			// RPC server shutting down.
			break out
		}
	}

	for _, c := range clients {
		c.Disconnect()
	}
	m.wg.Done()
}
