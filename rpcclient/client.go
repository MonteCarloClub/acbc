package rpcclient

import lg "log"

var (
	ModuleClient = make(map[string]*Client)
)

func init() {

	//ModuleClient["TestClient"]

	// Connect to local bitcoin core RPC server using HTTP POST mode.
	connCfg := &ConnConfig{
		Host: "127.0.0.1:8335",
		//Host:         "1341b702-7379-4d21-93a3-f1fffa730c23.mock.pstmn.io",
		User:         "jiajimeidou",
		Pass:         "12345678",
		HTTPPostMode: true,
		DisableTLS:   true,
	}
	// Notice the notification parameter is nil since notifications are
	// not supported in HTTP POST mode.
	testclient, err := New(connCfg)
	if err != nil {
		lg.Fatal(err)
	}
	//defer testclient.Shutdown()
	ModuleClient["moduleToTest"] = testclient
}
