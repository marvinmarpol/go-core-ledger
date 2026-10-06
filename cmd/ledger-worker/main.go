// Package main is the entry point for the ledger worker process.
package main

import (
	"fmt"
	"go-core-ledger/internal/worker"
)

func main() {
	if err := worker.Run(); err != nil {
		fmt.Println(err)
	}
}
