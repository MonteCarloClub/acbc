package cpuminer

// CPUMiner provides facilities for solving blocks (mining) using the CPU in
// a concurrency-safe manner.  It consists of two main goroutines -- a speed
// monitor and a controller for worker goroutines which generate and solve
// blocks.  The number of goroutines can be set via the SetMaxGoRoutines
// function, but the default is based on the number of processor cores in the
// system which is typically sufficient.
type CPUMiner struct {
	/*
		sync.Mutex
		g                 *mining.BlkTmplGenerator
		cfg               Config
		numWorkers        uint32
		started           bool
		discreteMining    bool
		submitBlockLock   sync.Mutex
		wg                sync.WaitGroup
		workerWg          sync.WaitGroup
		updateNumWorkers  chan struct{}
		queryHashesPerSec chan float64
		updateHashes      chan uint64
		speedMonitorQuit  chan struct{}
		quit              chan struct{}

	*/
}
