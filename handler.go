package main

import (
	"github.com/MonteCarloClub/acbc/rpcserver"
)

// handleGetBlockHash implements the getblockhash command.
func GetAccountData(s *rpcserver.RpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	// 这一行测试用
	return cmd, nil
}

func GetBlockData(s *rpcserver.RpcServer, cmd interface{}, closeChan <-chan struct{}) (interface{}, error) {
	// 这一行测试用
	return cmd, nil
}
