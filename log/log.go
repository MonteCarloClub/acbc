package log

import (
	"fmt"
	"github.com/btcsuite/btclog"
	"github.com/jrick/logrotate/rotator"
	"os"
	"path/filepath"
)

// logWriter implements an io.Writer that outputs to both standard output and
// the write-end pipe of an initialized log rotator.
type LogWriter struct{}

func (LogWriter) Write(p []byte) (n int, err error) {
	os.Stdout.Write(p)
	LogRotator.Write(p)
	return len(p), nil
}

// Loggers per subsystem.  A single backend logger is created and all subsytem
// loggers created from it will write to the backend.  When adding new
// subsystems, add the subsystem logger variable here and to the
// subsystemLoggers map.
//
// Loggers can not be used before the log rotator has been initialized with a
// log file.  This must be performed early during application startup by calling
// initLogRotator.
var (
	// backendLog is the logging backend used to create all subsystem loggers.
	// The backend must not be used before the log rotator has been initialized,
	// or data races and/or nil pointer dereferences will occur.
	backendLog = btclog.NewBackend(LogWriter{})

	// logRotator is one of the logging outputs.  It should be closed on
	// application shutdown.
	LogRotator *rotator.Rotator

	AdxrLog      = backendLog.Logger("ADXR")
	MmgrLog      = backendLog.Logger("AMGR")
	CmgrLog      = backendLog.Logger("CMGR")
	BcdbLog      = backendLog.Logger("BCDB")
	AcbcLog      = backendLog.Logger("ACBC")
	ChanLog      = backendLog.Logger("CHAN")
	DiscLog      = backendLog.Logger("DISC")
	IndxLog      = backendLog.Logger("INDX")
	MinrLog      = backendLog.Logger("MINR")
	PeerLog      = backendLog.Logger("PEER")
	RpcsLog      = backendLog.Logger("RPCS")
	ScrpLog      = backendLog.Logger("SCRP")
	SrvrLog      = backendLog.Logger("SRVR")
	SyncLog      = backendLog.Logger("SYNC")
	TxmpLog      = backendLog.Logger("TXMP")
	RPCCLientLog = backendLog.Logger("rpcclient")
)

// SubsystemLoggers maps each subsystem identifier to its associated logger.
var SubsystemLoggers = map[string]btclog.Logger{
	"ADXR": AdxrLog,
	"CMGR": CmgrLog,
	"BCDB": BcdbLog,
	"ACBC": AcbcLog,
	"CHAN": ChanLog,
	"DISC": DiscLog,
	"INDX": IndxLog,
	"MINR": MinrLog,
	"PEER": PeerLog,
	"RPCS": RpcsLog,
	"SCRP": ScrpLog,
	"SRVR": SrvrLog,
	"SYNC": SyncLog,
	"TXMP": TxmpLog,
}

// InitLogRotator initializes the logging rotater to write logs to logFile and
// create roll files in the same directory.  It must be called before the
// package-global log rotater variables are used.
func InitLogRotator(logFile string) {
	fmt.Println(logFile)
	logDir, _ := filepath.Split(logFile)
	fmt.Println(logDir)
	err := os.MkdirAll(logDir, 0700)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create log directory: %v\n", err)
		os.Exit(1)
	}
	r, err := rotator.New(logFile, 10*1024, false, 3)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create file rotator: %v\n", err)
		os.Exit(1)
	}

	LogRotator = r
}

// setLogLevel sets the logging level for provided subsystem.  Invalid
// subsystems are ignored.  Uninitialized subsystems are dynamically created as
// needed.
func SetLogLevel(subsystemID string, logLevel string) {
	// Ignore invalid subsystems.
	logger, ok := SubsystemLoggers[subsystemID]
	if !ok {
		return
	}

	// Defaults to info if the log level is invalid.
	level, _ := btclog.LevelFromString(logLevel)
	logger.SetLevel(level)
}

// SetLogLevels sets the log level for all subsystem loggers to the passed
// level.  It also dynamically creates the subsystem loggers as needed, so it
// can be used to initialize the logging system.
func SetLogLevels(logLevel string) {
	// Configure all sub-systems with the new logging level.  Dynamically
	// create loggers as needed.
	for subsystemID := range SubsystemLoggers {
		SetLogLevel(subsystemID, logLevel)
	}
}
