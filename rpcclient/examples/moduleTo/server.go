package main

import (
	"crypto/tls"
	"fmt"
	"github.com/MonteCarloClub/acbc/config"
	"github.com/MonteCarloClub/acbc/log"
	"github.com/MonteCarloClub/acbc/rpcserver"
	"github.com/MonteCarloClub/acbc/signal"
	"net"
	"sync"
	"time"
)

var (
	cfg       *config.Config
	rpcServer *rpcserver.RpcServer
)

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

func init() {
	tcfg, _, err := config.LoadConfig("8335")
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
		rpcListeners, err := setupRPCListeners()
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

		rpcserver.RpcHandlers["getblockhash"] = handleGetBlockHash

	}

}

func main() {
	// Get a channel that will be closed when a shutdown signal has been
	// triggered either from an OS signal such as SIGINT (Ctrl+C) or from
	// another subsystem such as the RPC server.
	wg := sync.WaitGroup{}
	interrupt := signal.InterruptListener()
	defer func() {
		log.AcbcLog.Infof("Gracefully shutting down the server...")
		if !cfg.DisableRPC {
			rpcServer.Stop()
		}
		//server.WaitForShutdown()
		log.SrvrLog.Infof("Server shutdown complete")
	}()
	if !cfg.DisableRPC {
		wg.Add(1)
		rpcServer.Start()
	}
	// Wait until the interrupt signal is received from an OS signal or
	// shutdown is requested through one of the subsystems such as the RPC
	// server.
	<-interrupt
	return
}

// handleGetBlockHash implements the getblockhash command.
func handleGetBlockHash(s *rpcserver.RpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {

	// 这一行测试用
	return cmd, nil

}
