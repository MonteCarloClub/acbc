package ffldb

import (
	"container/list"
	"github.com/MonteCarloClub/acbc/wire"
	"io"
	"sync"
)

// filer is an interface which acts very similar to a *os.File and is typically
// implemented by it.  It exists so the test code can provide mock files for
// properly testing corruption and file system issues.
type filer interface {
	io.Closer
	io.WriterAt
	io.ReaderAt
	Truncate(size int64) error
	Sync() error
}

// lockableFile represents a block file on disk that has been opened for either
// read or read/write access.  It also contains a read-write mutex to support
// multiple concurrent readers.
type lockableFile struct {
	sync.RWMutex
	file filer
}

// writeCursor represents the current file and offset of the block file on disk
// for performing all writes. It also contains a read-write mutex to support
// multiple concurrent readers which can reuse the file handle.
type writeCursor struct {
	sync.RWMutex

	// curFile is the current block file that will be appended to when
	// writing new blocks.
	curFile *lockableFile

	// curFileNum is the current block file number and is used to allow
	// readers to use the same open file handle.
	curFileNum uint32

	// curOffset is the offset in the current write block file where the
	// next new block will be written.
	curOffset uint32
}

// blockStore houses information used to handle reading and writing blocks (and
// part of blocks) into flat files with support for multiple concurrent readers.
type blockStore struct {
	// network is the specific network to use in the flat files for each
	// block.
	network wire.BitcoinNet

	// basePath is the base path used for the flat block files and metadata.
	basePath string

	// maxBlockFileSize is the maximum size for each file used to store
	// blocks.  It is defined on the store so the whitebox tests can
	// override the value.
	maxBlockFileSize uint32

	// The following fields are related to the flat files which hold the
	// actual blocks.   The number of open files is limited by maxOpenFiles.
	//
	// obfMutex protects concurrent access to the openBlockFiles map.  It is
	// a RWMutex so multiple readers can simultaneously access open files.
	//
	// openBlockFiles houses the open file handles for existing block files
	// which have been opened read-only along with an individual RWMutex.
	// This scheme allows multiple concurrent readers to the same file while
	// preventing the file from being closed out from under them.
	//
	// lruMutex protects concurrent access to the least recently used list
	// and lookup map.
	//
	// openBlocksLRU tracks how the open files are refenced by pushing the
	// most recently used files to the front of the list thereby trickling
	// the least recently used files to end of the list.  When a file needs
	// to be closed due to exceeding the the max number of allowed open
	// files, the one at the end of the list is closed.
	//
	// fileNumToLRUElem is a mapping between a specific block file number
	// and the associated list element on the least recently used list.
	//
	// Thus, with the combination of these fields, the database supports
	// concurrent non-blocking reads across multiple and individual files
	// along with intelligently limiting the number of open file handles by
	// closing the least recently used files as needed.
	//
	// NOTE: The locking order used throughout is well-defined and MUST be
	// followed.  Failure to do so could lead to deadlocks.  In particular,
	// the locking order is as follows:
	//   1) obfMutex
	//   2) lruMutex
	//   3) writeCursor mutex
	//   4) specific file mutexes
	//
	// None of the mutexes are required to be locked at the same time, and
	// often aren't.  However, if they are to be locked simultaneously, they
	// MUST be locked in the order previously specified.
	//
	// Due to the high performance and multi-read concurrency requirements,
	// write locks should only be held for the minimum time necessary.
	obfMutex         sync.RWMutex
	lruMutex         sync.Mutex
	openBlocksLRU    *list.List // Contains uint32 block file numbers.
	fileNumToLRUElem map[uint32]*list.Element
	openBlockFiles   map[uint32]*lockableFile

	// writeCursor houses the state for the current file and location that
	// new blocks are written to.
	writeCursor *writeCursor

	// These functions are set to openFile, openWriteFile, and deleteFile by
	// default, but are exposed here to allow the whitebox tests to replace
	// them when working with mock files.
	openFileFunc      func(fileNum uint32) (*lockableFile, error)
	openWriteFileFunc func(fileNum uint32) (filer, error)
	deleteFileFunc    func(fileNum uint32) error
}
