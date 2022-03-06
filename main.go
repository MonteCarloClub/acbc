package main

import (
	"fmt"
	"sync/atomic"
)

func main() {
	var id uint64 = 1

	fmt.Print(atomic.AddUint64(&id, 1))

}
