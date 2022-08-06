package main

import (
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/MonteCarloClub/acbc/config"
	"github.com/MonteCarloClub/acbc/log"
	"github.com/MonteCarloClub/acbc/rpcserver"
)

type register struct {
	name    string
	handler func(s *rpcserver.RpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error)
}

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

func SetupRPCListeners() ([]net.Listener, error) {
	listenFunc := net.Listen
	if !cfg.DisableTLS {
		if !config.FileExists(cfg.RPCKey) && !config.FileExists(cfg.RPCCert) {
			err := rpcserver.GenCertPair(cfg.RPCCert, cfg.RPCKey)
			if err != nil {
				return nil, err
			}
		}
		keypair, err := tls.LoadX509KeyPair(cfg.RPCCert, cfg.RPCKey)
		if err != nil {
			return nil, err
		}

		tlsConfig := tls.Config{
			Certificates: []tls.Certificate{keypair},
			MinVersion:   tls.VersionTLS12,
		}
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

func (s *server) rebroadcastHandler() {

}

// Start begins accepting connections from peers.
func (s *server) Start() {
	//Server startup time. Used for the uptime command for uptime calculation.
	s.startupTime = time.Now().Unix()

	if !cfg.DisableRPC {
		// q：
		s.wg.Add(1)

		go s.rebroadcastHandler()
		s.rpcServer.Start()
	}

}
func (s *server) Stop() error {

	if !cfg.DisableRPC {
		s.rpcServer.Stop()
	}
	close(s.quit)

	return nil
}

func newServer(port int, regs []register) {
	tcfg, _, err := config.LoadConfig(strconv.Itoa(port))
	if err != nil {
		fmt.Println(err)
		return
	}
	cfg = tcfg

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
		rpcListeners, err := SetupRPCListeners()
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		if len(rpcListeners) == 0 {
			fmt.Println("RPCS: No valid listen address")
			return
		}
		startupTime := time.Now().Unix()
		rpcServer, err = rpcserver.NewRPCServer(&rpcserver.RpcserverConfig{
			Listeners:   rpcListeners,
			StartupTime: startupTime,
			//ConnMgr:      &rpcConnManager{&s},
			//SyncMgr:      &rpcSyncMgr{&s, s.syncManager},
			Isproxy: false,
		}, rcfg)
		if err != nil {
			fmt.Println(err.Error())
			return
		}
		for _, reg := range regs {
			rpcserver.RpcHandlers[reg.name] = reg.handler
		}
	}
}
