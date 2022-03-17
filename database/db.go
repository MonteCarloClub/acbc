package database

import (
	"sync"
)

// db represents a collection of namespaces which are persisted and implements
// the database.DB interface.  All database access is performed through
// transactions which are obtained through the specific Namespace.
type db struct {
	writeLock sync.Mutex   // Limit to one write transaction at a time.
	closeLock sync.RWMutex // Make database close block while txns active.
	closed    bool         // Is the database closed?
	//store     *blockStore  // Handles read/writing blocks to flat files.
	//cache     *dbCache     // Cache layer which wraps underlying leveldb DB.
}
