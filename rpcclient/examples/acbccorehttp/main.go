package main

import (
	"fmt"
	"log"

	"github.com/MonteCarloClub/acbc/rpcclient"
)

func main() {
	// Connect to local bitcoin core RPC server using HTTP POST mode.
	connCfg := &rpcclient.ConnConfig{
		Host: "127.0.0.1:8334",
		//Host:         "1341b702-7379-4d21-93a3-f1fffa730c23.mock.pstmn.io",
		User:         "jiajimeidou",
		Pass:         "12345678",
		HTTPPostMode: true,
		DisableTLS:   false,
	}
	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	client, err := rpcclient.New(connCfg, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown()

	// Get the current block count.
	fmt.Println("before")
	blockhash, err := client.GetBlockHash(1111)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("after")
	log.Printf("Block hash: %s", blockhash)
}
