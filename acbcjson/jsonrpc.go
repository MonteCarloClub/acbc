package acbcjson

import (
	"encoding/json"
	"fmt"
)

// RPCVersion is a type to indicate RPC versions.
type RPCVersion string

const (
	// version 1 of rpc
	RpcVersion1 RPCVersion = RPCVersion("1.0")
	// version 2 of rpc
	RpcVersion2 RPCVersion = RPCVersion("2.0")
)

var validRpcVersions = []RPCVersion{RpcVersion1, RpcVersion2}

// check if the rpc version is a valid version
func (r RPCVersion) IsValid() bool {
	for _, version := range validRpcVersions {
		if version == r {
			return true
		}
	}
	return false
}

// IsValidIDType checks that the ID field (which can go in any of the JSON-RPC
// requests, responses, or notifications) is valid.  JSON-RPC 1.0 allows any
// valid JSON type.  JSON-RPC 2.0 (which bitcoind follows for some parts) only
// allows string, number, or null, so this function restricts the allowed types
// to that list.  This function is only provided in case the caller is manually
// marshalling for some reason.    The functions which accept an ID in this
// package already call this function to ensure the provided id is valid.
func IsValidIDType(id interface{}) bool {
	switch id.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		string,
		nil:
		return true
	default:
		return false
	}
}

// Request is a type for raw JSON-RPC 1.0 requests.  The Method field identifies
// the specific command type which in turns leads to different parameters.
// Callers typically will not use this directly since this package provides a
// statically typed command infrastructure which handles creation of these
// requests, however this struct it being exported in case the caller wants to
// construct raw requests for some reason.
type Request struct {
	Jsonrpc RPCVersion        `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
	ID      interface{}       `json:"id"`
}

// NewRequest returns a new JSON-RPC request object given the provided rpc
// version, id, method, and parameters.  The parameters are marshalled into a
// json.RawMessage for the Params field of the returned request object. This
// function is only provided in case the caller wants to construct raw requests
// for some reason. Typically callers will instead want to create a registered
// concrete command type with the NewCmd or New<Foo>Cmd functions and call the
// MarshalCmd function with that command to generate the marshalled JSON-RPC
// request.
// 给定提供的 rpc 版本、id、方法和参数，NewRequest 返回一个新的 JSON-RPC 请求对象。
// 参数被编组到返回请求对象的 Params 字段的 json.RawMessage 中。仅在调用者出于某种原因想要构造原始请求的情况下
// 才提供此功能。通常，调用者会希望使用 NewCmd 或 New<Foo>Cmd 函数创建已注册的具体命令类型，并使用该命令
// 调用 MarshalCmd 函数以生成编组的 JSON-RPC 要求。
func NewRequest(rpcVersion RPCVersion, id interface{}, method string, params []interface{}) (*Request, error) {
	// default to JSON-RPC 1.0 if RPC type is not specified
	if !rpcVersion.IsValid() {
		str := fmt.Sprintf("rpcversion '%s' is invalid", rpcVersion)
		return nil, makeError(ErrInvalidType, str)
	}

	if !IsValidIDType(id) {
		str := fmt.Sprintf("the id of type '%T' is invalid", id)
		return nil, makeError(ErrInvalidType, str)
	}

	rawParams := make([]json.RawMessage, 0, len(params))
	for _, param := range params {
		marshalledParam, err := json.Marshal(param)
		if err != nil {
			return nil, err
		}
		rawMessage := json.RawMessage(marshalledParam)
		rawParams = append(rawParams, rawMessage)
	}

	return &Request{
		Jsonrpc: rpcVersion,
		ID:      id,
		Method:  method,
		Params:  rawParams,
	}, nil
}
