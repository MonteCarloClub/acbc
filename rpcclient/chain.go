package rpcclient

import (
	"fmt"
	"github.com/MonteCarloClub/acbc/acbcjson"
)

// FutureGetResult is a future promise to deliver the result of a
type FutureGetResult chan *Response

// GetBlockHashAsync returns an instance of a type that can be used to get the
// result of the RPC at some future time by invoking the Receive function on the
// returned instance.
//
// See GetBlockHash for the blocking version and more details.
func (c *Client) GetBlockHashAsync(moduleFrom string, moduleTo string, blockHeight int64) FutureGetResult {
	cmd := acbcjson.NewGetBlockHashCmd(blockHeight)
	return c.SendCmd(moduleFrom, moduleTo, cmd)
}

// Receive waits for the Response promised by the future and returns the hash of
// the block in the best block chain at the given height.
func (r FutureGetResult) Receive() (string, error) {
	resp, err := ReceiveFuture(r)
	if err != nil {
		return "", err
	}
	fmt.Printf("%+v\n", resp)
	return string(resp.Result), nil
}

// GetBlockHash returns the hash of the block in the best block chain at the
// given height.
func (c *Client) GetBlockHash(blockHeight int64, moduleFrom string, moduleTo string) (string, error) {
	return c.GetBlockHashAsync(moduleFrom, moduleTo, blockHeight).Receive()
}
