//NOTE: This file is intended to house the RPC commands that are supported by a chain server.

package acbcjson

// GetBlockHashCmd defines the getblockhash JSON-RPC command.
type GetBlockHashCmd struct {
	Index int64
}

// NewGetBlockHashCmd returns a new instance which can be used to issue a getblockhash JSON-RPC command.
func NewGetBlockHashCmd(index int64) *GetBlockHashCmd {
	return &GetBlockHashCmd{
		Index: index,
	}
}

func init() {
	// re： 导入包时，go会自动执行包的init函数
	// No special flags for commands in this file.
	flags := UsageFlag(0)

	MustRegisterCmd("getblockhash", (*GetBlockHashCmd)(nil), flags)
}
