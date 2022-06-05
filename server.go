package main

import (
	"crypto/tls"
	"errors"
	"github.com/MonteCarloClub/acbc/config"
	"github.com/MonteCarloClub/acbc/log"
	"github.com/MonteCarloClub/acbc/rpcserver"
	"net"
	"sync"
	"time"
)

// server provides a bitcoin server for handling communications to and from
// bitcoin peers.
type server struct {
	// The following variables must only be used atomically.
	// Putting the uint64s first makes them 64-bit aligned for 32-bit systems.
	bytesReceived uint64 // Total bytes received from all peers since start.
	bytesSent     uint64 // Total bytes sent by all peers since start.
	started       int32
	shutdown      int32
	shutdownSched int32
	startupTime   int64

	/*
		chainParams          *chaincfg.Params
		addrManager          *addrmgr.AddrManager
		connManager          *connmgr.ConnManager
		sigCache             *txscript.SigCache
		hashCache            *txscript.HashCache
	*/
	rpcServer            *rpcserver.RpcServer
	modifyRebroadcastInv chan interface{}
	newPeers             chan *serverPeer
	donePeers            chan *serverPeer
	banPeers             chan *serverPeer
	query                chan interface{}
	wg                   sync.WaitGroup
	quit                 chan struct{}
	//nat                  NAT

	// agentBlacklist is a list of blacklisted substrings by which to filter
	// user agents.
	agentBlacklist []string

	// agentWhitelist is a list of whitelisted user agent substrings, no
	// whitelisting will be applied if the list is empty or nil.
	agentWhitelist []string
}

// serverPeer extends the peer to maintain state shared by the server and
// the blockmanager.
type serverPeer struct {
	// The following variables must only be used atomically
	feeFilter int64

	//connReq        *connmgr.ConnReq
	server     *server
	persistent bool
	//continueHash   *chainhash.Hash
	relayMtx       sync.Mutex
	disableRelayTx bool
	sentAddrs      bool
	isWhitelisted  bool
	//filter         *bloom.Filter
	addressesMtx   sync.RWMutex
	knownAddresses map[string]struct{}
	quit           chan struct{}
	// The following chans are used to sync blockmanager and server.
	txProcessed    chan struct{}
	blockProcessed chan struct{}
}

// onionAddr implements the net.Addr interface and represents a tor address.
type onionAddr struct {
	addr string
}

// WaitForShutdown blocks until the main listener and peer handlers are stopped.
func (s *server) WaitForShutdown() {
	s.wg.Wait()
}

// setupRPCListeners returns a slice of listeners that are configured for use
// with the RPC server depending on the configuration settings for listen
// addresses and TLS.
func setupRPCListeners() ([]net.Listener, error) {
	// Setup TLS if not disabled.
	// 函数也是一种类型，可以把该函数赋值给变量，通过变量调用
	listenFunc := net.Listen
	if !cfg.DisableTLS {
		// Generate the TLS cert and key file if both don't already
		// exist.
		if !config.FileExists(cfg.RPCKey) && !config.FileExists(cfg.RPCCert) {
			err := rpcserver.GenCertPair(cfg.RPCCert, cfg.RPCKey)
			if err != nil {
				return nil, err
			}
		}
		// LoadX509KeyPair 从一对文件中读取并解析公钥/私钥对。这些文件必须包含 PEM 编码数据。
		// 证书文件可以包含在叶证书之后的中间证书以形成证书链。成功返回时，Certificate.Leaf 将为 nil，因为不保留已解析的证书形式。
		// https://colobu.com/2016/06/07/simple-golang-tls-examples/
		keypair, err := tls.LoadX509KeyPair(cfg.RPCCert, cfg.RPCKey)
		if err != nil {
			return nil, err
		}

		tlsConfig := tls.Config{
			Certificates: []tls.Certificate{keypair},
			MinVersion:   tls.VersionTLS12,
		}
		// Change the standard net.Listen function to the tls one.
		listenFunc = func(net string, laddr string) (net.Listener, error) {
			return tls.Listen(net, laddr, &tlsConfig)
		}
	}
	netAddrs, err := config.ParseListeners(cfg.RPCListeners)
	if err != nil {
		return nil, err
	}
	listeners := make([]net.Listener, 0, len(netAddrs))
	for _, addr := range netAddrs {
		listener, err := listenFunc(addr.Network(), addr.String())
		if err != nil {
			log.RpcsLog.Warnf("Can't listen on %s: %v", addr, err)
			continue
		}
		listeners = append(listeners, listener)
	}
	return listeners, nil
}

func newServer() (*server, error) {

	s := server{
		//chainParams:          chainParams,
		//addrManager:          amgr,
		query: make(chan interface{}),

		quit:                 make(chan struct{}),
		modifyRebroadcastInv: make(chan interface{}),
	}

	if !cfg.DisableRPC {

		rcfg := &rpcserver.Rpcconfig{
			cfg.DisableRPC,
			cfg.DisableTLS,
			cfg.RPCCert,
			cfg.RPCKey,
			cfg.RPCLimitPass,
			cfg.RPCLimitUser,
			cfg.RPCListeners,
			cfg.RPCMaxClients,
			cfg.RPCMaxConcurrentReqs,
			cfg.RPCMaxWebsockets,
			cfg.RPCQuirks,
			cfg.RPCPass,
			cfg.RPCUser,
		}

		// Setup listeners for the configured RPC listen addresses and
		// TLS settings.
		rpcListeners, err := setupRPCListeners()
		if err != nil {
			return nil, err
		}
		if len(rpcListeners) == 0 {
			return nil, errors.New("RPCS: No valid listen address")
		}
		s.rpcServer, err = rpcserver.NewRPCServer(&rpcserver.RpcserverConfig{
			Listeners:   rpcListeners,
			StartupTime: s.startupTime,
			//ConnMgr:      &rpcConnManager{&s},
			//SyncMgr:      &rpcSyncMgr{&s, s.syncManager},
			Isproxy: true,
		}, rcfg)
		if err != nil {
			return nil, err
		}
		// Signal process shutdown when the RPC server requests it.
		go func() {
			// todo： 待分析
			//<-s.rpcServer.RequestedProcessShutdown()
			//shutdownRequestChannel <- struct{}{}
		}()
	}

	return &s, nil
}

// rebroadcastHandler keeps track of user submitted inventories that we have
// sent out but have not yet made it into a block. We periodically rebroadcast
// them in case our peers restarted or otherwise lost track of them.
func (s *server) rebroadcastHandler() {
	/*

			// Wait 5 min before first tx rebroadcast.
			timer := time.NewTimer(5 * time.Minute)
			pendingInvs := make(map[wire.InvVect]interface{})

		out:
			for {
				select {
				case riv := <-s.modifyRebroadcastInv:
					switch msg := riv.(type) {
					// Incoming InvVects are added to our map of RPC txs.
					case broadcastInventoryAdd:
						pendingInvs[*msg.invVect] = msg.data

					// When an InvVect has been added to a block, we can
					// now remove it, if it was present.
					case broadcastInventoryDel:
						delete(pendingInvs, *msg)
					}

				case <-timer.C:
					// Any inventory we have has not made it into a block
					// yet. We periodically resubmit them until they have.
					for iv, data := range pendingInvs {
						ivCopy := iv
						s.RelayInventory(&ivCopy, data)
					}

					// Process at a random time up to 30mins (in seconds)
					// in the future.
					timer.Reset(time.Second *
						time.Duration(randomUint16Number(1800)))

				case <-s.quit:
					break out
				}
			}

			timer.Stop()

			// Drain channels before exiting so nothing is left waiting around
			// to send.
		cleanup:
			for {
				select {
				case <-s.modifyRebroadcastInv:
				default:
					break cleanup
				}
			}
			s.wg.Done()

	*/
}

// Start begins accepting connections from peers.
func (s *server) Start() {
	//Server startup time. Used for the uptime command for uptime calculation.
	s.startupTime = time.Now().Unix()
	/*
		// Already started?
		if atomic.AddInt32(&s.started, 1) != 1 {
			return
		}

		srvrLog.Trace("Starting server")

		 Server startup time. Used for the uptime command for uptime calculation.
		s.startupTime = time.Now().Unix()

		// Start the peer handler which in turn starts the address and block
		// managers.
		s.wg.Add(1)
		go s.peerHandler()

		if s.nat != nil {
			s.wg.Add(1)
			go s.upnpUpdateThread()
		}

	*/

	if !cfg.DisableRPC {
		// q：
		s.wg.Add(1)

		// Start the rebroadcastHandler, which ensures user tx received by
		// the RPC server are rebroadcast until being included in a block.
		go s.rebroadcastHandler()
		s.rpcServer.Start()
	}
	/*
		// Start the CPU miner if generation is enabled.
		if cfg.Generate {
			s.cpuMiner.Start()
		}

	*/
}

// Stop gracefully shuts down the server by stopping and disconnecting all
// peers and the main listener.
func (s *server) Stop() error {
	/*
		// Make sure this only happens once.
		if atomic.AddInt32(&s.shutdown, 1) != 1 {
			srvrLog.Infof("Server is already in the process of shutting down")
			return nil
		}

		srvrLog.Warnf("Server shutting down")

		// Stop the CPU miner if needed
		s.cpuMiner.Stop()
	*/
	// Shutdown the RPC server if it's not disabled.
	if !cfg.DisableRPC {
		s.rpcServer.Stop()
	}
	/*
		// Save fee estimator state in the database.
		s.db.Update(func(tx database.Tx) error {
			metadata := tx.Metadata()
			metadata.Put(mempool.EstimateFeeDatabaseKey, s.feeEstimator.Save())

			return nil
		})
	*/
	// Signal the remaining goroutines to quit.
	close(s.quit)

	return nil
}
