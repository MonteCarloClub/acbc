package acbcjson

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
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
func MarshalCmd(moduleFrom string, moduleTo string, id interface{}, cmd interface{}) ([]byte, error) {
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
	rawCmd, err := NewRequest(moduleFrom, moduleTo, id, method, params)
	if err != nil {
		return nil, err
	}
	return json.Marshal(rawCmd)
}

// checkNumParams ensures the supplied number of params is at least the minimum
// required number for the command and less than the maximum allowed.
func checkNumParams(numParams int, info *methodInfo) error {
	if numParams < info.numReqParams || numParams > info.maxParams {
		if info.numReqParams == info.maxParams {
			str := fmt.Sprintf("wrong number of params (expected "+
				"%d, received %d)", info.numReqParams,
				numParams)
			return makeError(ErrNumParams, str)
		}

		str := fmt.Sprintf("wrong number of params (expected "+
			"between %d and %d, received %d)", info.numReqParams,
			info.maxParams, numParams)
		return makeError(ErrNumParams, str)
	}

	return nil
}

// populateDefaults populates default values into any remaining optional struct
// fields that did not have parameters explicitly provided.  The caller should
// have previously checked that the number of parameters being passed is at
// least the required number of parameters to avoid unnecessary work in this
// function, but since required fields never have default values, it will work
// properly even without the check.
func populateDefaults(numParams int, info *methodInfo, rv reflect.Value) {
	// When there are no more parameters left in the supplied parameters,
	// any remaining struct fields must be optional.  Thus, populate them
	// with their associated default value as needed.
	for i := numParams; i < info.maxParams; i++ {
		rvf := rv.Field(i)
		if defaultVal, ok := info.defaults[i]; ok {
			rvf.Set(defaultVal)
		}
	}
}

// UnmarshalCmd unmarshals a JSON-RPC request into a suitable concrete command
// so long as the method type contained within the marshalled request is
// registered.
func UnmarshalCmd(r *Request) (interface{}, error) {
	registerLock.RLock()
	rtp, ok := MethodToConcreteType[r.Method]
	info := MethodToInfo[r.Method]
	registerLock.RUnlock()
	if !ok {
		str := fmt.Sprintf("%q is not registered", r.Method)
		return nil, makeError(ErrUnregisteredMethod, str)
	}
	rt := rtp.Elem()
	rvp := reflect.New(rt)
	rv := rvp.Elem()

	// Ensure the number of parameters are correct.
	numParams := len(r.Params)
	if err := checkNumParams(numParams, &info); err != nil {
		return nil, err
	}

	// Loop through each of the struct fields and unmarshal the associated
	// parameter into them.
	for i := 0; i < numParams; i++ {
		rvf := rv.Field(i)
		// Unmarshal the parameter into the struct field.
		concreteVal := rvf.Addr().Interface()
		if err := json.Unmarshal(r.Params[i], &concreteVal); err != nil {
			// The most common error is the wrong type, so
			// explicitly detect that error and make it nicer.
			fieldName := strings.ToLower(rt.Field(i).Name)
			if jerr, ok := err.(*json.UnmarshalTypeError); ok {
				str := fmt.Sprintf("parameter #%d '%s' must "+
					"be type %v (got %v)", i+1, fieldName,
					jerr.Type, jerr.Value)
				return nil, makeError(ErrInvalidType, str)
			}

			// Fallback to showing the underlying error.
			str := fmt.Sprintf("parameter #%d '%s' failed to "+
				"unmarshal: %v", i+1, fieldName, err)
			return nil, makeError(ErrInvalidType, str)
		}
	}

	// When there are less supplied parameters than the total number of
	// params, any remaining struct fields must be optional.  Thus, populate
	// them with their associated default value as needed.
	if numParams < info.maxParams {
		populateDefaults(numParams, &info, rv)
	}

	return rvp.Interface(), nil
}
