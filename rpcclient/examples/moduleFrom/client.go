package main

import (
	"fmt"
	"github.com/MonteCarloClub/acbc/acbcjson"
	"github.com/MonteCarloClub/acbc/rpcclient"
	lg "log"
)

var (
	ProxyClient = make(map[string]*rpcclient.Client)
)

func main() {

	proxyclient1 := ProxyClient["proxyclient1"]
	proxyclient1.Start()
	defer proxyclient1.Shutdown()
	//
	// Get the current block count.
	fmt.Println("before")
	blockhash, err := GetBlockHash(proxyclient1, 1111, "moduleFromTest", "moduleToTest")
	if err != nil {
		lg.Fatal(err)
	}
	fmt.Println("after")
	lg.Printf("Block hash: %s", blockhash)

}

func init() {
	//以下初始化
	connCfg := &rpcclient.ConnConfig{
		Host:         "127.0.0.1:8334", //代理的ip和端口
		User:         "jiajimeidou",
		Pass:         "12345678",
		HTTPPostMode: true,
		DisableTLS:   true,
	}
	proxyclient1, err := rpcclient.New(connCfg)
	if err != nil {
		lg.Fatal(err)
	}
	ProxyClient["proxyclient1"] = proxyclient1
}

func GetBlockHash(client *rpcclient.Client, blockHeight int64, moduleFrom string, moduleTo string) (string, error) {
	cmd := acbcjson.NewGetBlockHashCmd(blockHeight)
	resChan := client.SendRequest(moduleFrom, moduleTo, cmd)
	res, err := rpcclient.ReceiveFuture(resChan)
	if err != nil {
		return "nil", err
	}
	return string(res.Result), nil
}
