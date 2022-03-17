package acbcjson

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// makeParams creates a slice of interface values for the given struct.
func makeParams(rt reflect.Type, rv reflect.Value) []interface{} {
	numFields := rt.NumField()
	params := make([]interface{}, 0, numFields)
	lastParam := -1
	for i := 0; i < numFields; i++ {
		rtf := rt.Field(i)
		rvf := rv.Field(i)
		params = append(params, rvf.Interface())
		if rtf.Type.Kind() == reflect.Ptr {
			if rvf.IsNil() {
				// Omit optional null params unless a non-null param follows
				// q： ？？？？
				continue
			}
		}
		lastParam = i
	}
	return params[:lastParam+1]
}

// MarshalCmd marshals the passed command to a JSON-RPC request byte slice that
// is suitable for transmission to an RPC server.  The provided command type
// must be a registered type.  All commands provided by this package are
// registered by default.
func MarshalCmd(rpcVersion RPCVersion, id interface{}, cmd interface{}) ([]byte, error) {
	// Look up the cmd type and error out if not registered.
	rt := reflect.TypeOf(cmd)
	registerLock.RLock()
	method, ok := concreteTypeToMethod[rt]
	registerLock.RUnlock()
	if !ok {
		str := fmt.Sprintf("%q is not registered", method)
		return nil, makeError(ErrUnregisteredMethod, str)
	}

	// The provided command must not be nil.
	rv := reflect.ValueOf(cmd)
	if rv.IsNil() {
		str := "the specified command is nil"
		return nil, makeError(ErrInvalidType, str)
	}

	// Create a slice of interface values in the order of the struct fields
	// while respecting pointer fields as optional params and only adding
	// them if they are non-nil.
	// q： 为什么先创建一个接口切片，然后对这个接口切片中的每个field序列化，并且将指针视为可选参数，仅当它们非空时才添加它们？
	params := makeParams(rt.Elem(), rv.Elem())

	// Generate and marshal the final JSON-RPC request.
	rawCmd, err := NewRequest(rpcVersion, id, method, params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rawCmd)
}
