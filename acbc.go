package main

import (
	"fmt"
	"os"
	"runtime"
)

var (
	cfg *config
)

// winServiceMain is only invoked on Windows.  It detects when btcd is running
// as a service and reacts accordingly.
// todo: 处理windows的情况
var winServiceMain func() (bool, error)

func main() {
	// Call serviceMain on Windows to handle running as a service.  When
	// the return isService flag is true, exit now since we ran as a
	// service.  Otherwise, just fall through to normal operation.
	if runtime.GOOS == "windows" {
		isService, err := winServiceMain()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if isService {
			os.Exit(0)
		}
	}

	// Work around defer not working after os.Exit()
	if err := acbcMain(nil); err != nil {
		os.Exit(1)
	}
}

// acbcMain
func acbcMain(serverChan chan<- *server) error {
	// Load configuration and parse command line.  This function also
	// initializes logging and configures it accordingly.
	tcfg, _, err := loadConfig()
	if err != nil {
		return err
	}
	cfg = tcfg
	defer func() {
		if logRotator != nil {
			logRotator.Close()
		}
	}()

	// Get a channel that will be closed when a shutdown signal has been
	// triggered either from an OS signal such as SIGINT (Ctrl+C) or from
	// another subsystem such as the RPC server.
	interrupt := interruptListener()
	defer acbcLog.Info("Shutdown complete")

	// Show version at startup.
	acbcLog.Infof("Version %s", version())

	//todo： 启动其他

	// Create server and start it.
	server, err := newServer()
	if err != nil {
		// TODO: this logging could do with some beautifying.
		//acbcLog.Errorf("Unable to start server on %v: %v",
		//cfg.Listeners, err)
		return err
	}
	defer func() {
		acbcLog.Infof("Gracefully shutting down the server...")
		server.Stop()
		//server.WaitForShutdown()
		srvrLog.Infof("Server shutdown complete")
	}()
	server.Start()
	if serverChan != nil {
		serverChan <- server
	}

	// Wait until the interrupt signal is received from an OS signal or
	// shutdown is requested through one of the subsystems such as the RPC
	// server.
	<-interrupt
	return nil
}
