package rpcclient

import (
	"encoding/json"
	"github.com/MonteCarloClub/acbc/chaincfg/chainhash"
	"github.com/MonteCarloClub/acbc/wire"
	"github.com/btcsuite/btcd/btcjson"
	"github.com/btcsuite/btcd/btcutil"
	"time"
)

// NotificationHandlers defines callback function pointers to invoke with
// notifications.  Since all of the functions are nil by default, all
// notifications are effectively ignored until their handlers are set to a
// concrete callback.
//
// NOTE: Unless otherwise documented, these handlers must NOT directly call any
// blocking calls on the client instance since the input reader goroutine blocks
// until the callback has completed.  Doing so will result in a deadlock
// situation.
type NotificationHandlers struct {
	// OnClientConnected is invoked when the client connects or reconnects
	// to the RPC server.  This callback is run async with the rest of the
	// notification handlers, and is safe for blocking client requests.
	OnClientConnected func()

	// OnBlockConnected is invoked when a block is connected to the longest
	// (best) chain.  It will only be invoked if a preceding call to
	// NotifyBlocks has been made to register for the notification and the
	// function is non-nil.
	//
	// Deprecated: Use OnFilteredBlockConnected instead.
	OnBlockConnected func(hash *chainhash.Hash, height int32, t time.Time)

	// OnFilteredBlockConnected is invoked when a block is connected to the
	// longest (best) chain.  It will only be invoked if a preceding call to
	// NotifyBlocks has been made to register for the notification and the
	// function is non-nil.  Its parameters differ from OnBlockConnected: it
	// receives the block's height, header, and relevant transactions.
	OnFilteredBlockConnected func(height int32, header *wire.BlockHeader,
		txs []*btcutil.Tx)

	// OnBlockDisconnected is invoked when a block is disconnected from the
	// longest (best) chain.  It will only be invoked if a preceding call to
	// NotifyBlocks has been made to register for the notification and the
	// function is non-nil.
	//
	// Deprecated: Use OnFilteredBlockDisconnected instead.
	OnBlockDisconnected func(hash *chainhash.Hash, height int32, t time.Time)

	// OnFilteredBlockDisconnected is invoked when a block is disconnected
	// from the longest (best) chain.  It will only be invoked if a
	// preceding NotifyBlocks has been made to register for the notification
	// and the call to function is non-nil.  Its parameters differ from
	// OnBlockDisconnected: it receives the block's height and header.
	OnFilteredBlockDisconnected func(height int32, header *wire.BlockHeader)

	// OnRecvTx is invoked when a transaction that receives funds to a
	// registered address is received into the memory pool and also
	// connected to the longest (best) chain.  It will only be invoked if a
	// preceding call to NotifyReceived, Rescan, or RescanEndHeight has been
	// made to register for the notification and the function is non-nil.
	//
	// Deprecated: Use OnRelevantTxAccepted instead.
	OnRecvTx func(transaction *btcutil.Tx, details *btcjson.BlockDetails)

	// OnRedeemingTx is invoked when a transaction that spends a registered
	// outpoint is received into the memory pool and also connected to the
	// longest (best) chain.  It will only be invoked if a preceding call to
	// NotifySpent, Rescan, or RescanEndHeight has been made to register for
	// the notification and the function is non-nil.
	//
	// NOTE: The NotifyReceived will automatically register notifications
	// for the outpoints that are now "owned" as a result of receiving
	// funds to the registered addresses.  This means it is possible for
	// this to invoked indirectly as the result of a NotifyReceived call.
	//
	// Deprecated: Use OnRelevantTxAccepted instead.
	OnRedeemingTx func(transaction *btcutil.Tx, details *btcjson.BlockDetails)

	// OnRelevantTxAccepted is invoked when an unmined transaction passes
	// the client's transaction filter.
	//
	// NOTE: This is a btcsuite extension ported from
	// github.com/decred/dcrrpcclient.
	OnRelevantTxAccepted func(transaction []byte)

	// OnRescanFinished is invoked after a rescan finishes due to a previous
	// call to Rescan or RescanEndHeight.  Finished rescans should be
	// signaled on this notification, rather than relying on the return
	// result of a rescan request, due to how btcd may send various rescan
	// notifications after the rescan request has already returned.
	//
	// Deprecated: Not used with RescanBlocks.
	OnRescanFinished func(hash *chainhash.Hash, height int32, blkTime time.Time)

	// OnRescanProgress is invoked periodically when a rescan is underway.
	// It will only be invoked if a preceding call to Rescan or
	// RescanEndHeight has been made and the function is non-nil.
	//
	// Deprecated: Not used with RescanBlocks.
	OnRescanProgress func(hash *chainhash.Hash, height int32, blkTime time.Time)

	// OnTxAccepted is invoked when a transaction is accepted into the
	// memory pool.  It will only be invoked if a preceding call to
	// NotifyNewTransactions with the verbose flag set to false has been
	// made to register for the notification and the function is non-nil.
	OnTxAccepted func(hash *chainhash.Hash, amount btcutil.Amount)

	// OnTxAccepted is invoked when a transaction is accepted into the
	// memory pool.  It will only be invoked if a preceding call to
	// NotifyNewTransactions with the verbose flag set to true has been
	// made to register for the notification and the function is non-nil.
	OnTxAcceptedVerbose func(txDetails *btcjson.TxRawResult)

	// OnBtcdConnected is invoked when a wallet connects or disconnects from
	// btcd.
	//
	// This will only be available when client is connected to a wallet
	// server such as btcwallet.
	OnBtcdConnected func(connected bool)

	// OnAccountBalance is invoked with account balance updates.
	//
	// This will only be available when speaking to a wallet server
	// such as btcwallet.
	OnAccountBalance func(account string, balance btcutil.Amount, confirmed bool)

	// OnWalletLockState is invoked when a wallet is locked or unlocked.
	//
	// This will only be available when client is connected to a wallet
	// server such as btcwallet.
	OnWalletLockState func(locked bool)

	// OnUnknownNotification is invoked when an unrecognized notification
	// is received.  This typically means the notification handling code
	// for this package needs to be updated for a new notification type or
	// the caller is using a custom notification this package does not know
	// about.
	OnUnknownNotification func(method string, params []json.RawMessage)
}
