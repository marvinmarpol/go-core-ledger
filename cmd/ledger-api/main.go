// Package main is the entry point for the ledger API server.
package main

import (
	"fmt"
	"go-core-ledger/internal/app"
)

func main() {
	err := app.Run()
	if err != nil {
		fmt.Println(err)
	}
}
