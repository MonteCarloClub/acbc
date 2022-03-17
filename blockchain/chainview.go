package blockchain

import "sync"

// chainView provides a flat view of a specific branch of the block chain from
// its tip back to the genesis block and provides various convenience functions
// for comparing chains.
//
// For example, assume a block chain with a side chain as depicted below:
//   genesis -> 1 -> 2 -> 3 -> 4  -> 5 ->  6  -> 7  -> 8
//                         \-> 4a -> 5a -> 6a
//
// The chain view for the branch ending in 6a consists of:
//   genesis -> 1 -> 2 -> 3 -> 4a -> 5a -> 6a
type chainView struct {
	mtx   sync.Mutex
	nodes []*blockNode
}

// nodeByHeight returns the block node at the specified height.  Nil will be
// returned if the height does not exist.  This only differs from the exported
// version in that it is up to the caller to ensure the lock is held.
//
// This function MUST be called with the view mutex locked (for reads).
func (c *chainView) nodeByHeight(height int32) *blockNode {
	if height < 0 || height >= int32(len(c.nodes)) {
		return nil
	}

	return c.nodes[height]
}

// NodeByHeight returns the block node at the specified height.  Nil will be
// returned if the height does not exist.
//
// This function is safe for concurrent access.
func (c *chainView) NodeByHeight(height int32) *blockNode {
	c.mtx.Lock()
	node := c.nodeByHeight(height)
	c.mtx.Unlock()
	return node
}
