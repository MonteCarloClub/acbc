package acbcjson

import (
	"fmt"
	"reflect"
)

// CmdMethod returns the method for the passed command.  The provided command
// type must be a registered type.  All commands provided by this package are
// registered by default.
func CmdMethod(cmd interface{}) (string, error) {
	// Look up the cmd type and error out if not registered.
	rt := reflect.TypeOf(cmd)
	// q: 为什么要上锁
	registerLock.RLock()
	method, ok := concreteTypeToMethod[rt]
	registerLock.RUnlock()
	if !ok {
		str := fmt.Sprintf("%q is not registered", method)
		return "", makeError(ErrUnregisteredMethod, str)
	}

	return method, nil
}
