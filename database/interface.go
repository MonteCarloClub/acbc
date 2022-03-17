package database

// DB provides a generic interface that is used to store bitcoin blocks and
// related metadata.  This interface is intended to be agnostic to the actual
// mechanism used for backend data storage.  The RegisterDriver function can be
// used to add a new backend data storage method.
//
// This interface is divided into two distinct categories of functionality.
//
// The first category is atomic metadata storage with bucket support.  This is
// accomplished through the use of database transactions.
//
// The second category is generic block storage.  This functionality is
// intentionally separate because the mechanism used for block storage may or
// may not be the same mechanism used for metadata storage.  For example, it is
// often more efficient to store the block data as flat files while the metadata
// is kept in a database.  However, this interface aims to be generic enough to
// support blocks in the database too, if needed by a particular backend.
type DB interface {
	// Type returns the database driver type the current database instance
	// was created with.
	Type() string

	// Begin starts a transaction which is either read-only or read-write
	// depending on the specified flag.  Multiple read-only transactions
	// can be started simultaneously while only a single read-write
	// transaction can be started at a time.  The call will block when
	// starting a read-write transaction when one is already open.
	//
	// NOTE: The transaction must be closed by calling Rollback or Commit on
	// it when it is no longer needed.  Failure to do so can result in
	// unclaimed memory and/or inablity to close the database due to locks
	// depending on the specific database implementation.
	//Begin(writable bool) (Tx, error)

	// View invokes the passed function in the context of a managed
	// read-only transaction.  Any errors returned from the user-supplied
	// function are returned from this function.
	//
	// Calling Rollback or Commit on the transaction passed to the
	// user-supplied function will result in a panic.
	//View(fn func(tx Tx) error) error

	// Update invokes the passed function in the context of a managed
	// read-write transaction.  Any errors returned from the user-supplied
	// function will cause the transaction to be rolled back and are
	// returned from this function.  Otherwise, the transaction is committed
	// when the user-supplied function returns a nil error.
	//
	// Calling Rollback or Commit on the transaction passed to the
	// user-supplied function will result in a panic.
	//Update(fn func(tx Tx) error) error

	// Close cleanly shuts down the database and syncs all data.  It will
	// block until all database transactions have been finalized (rolled
	// back or committed).
	Close() error
}
