package main

import (
	"fmt"
	"os"

	"github.com/whiter001/agent-go/internal/app"
)

func main() {
	if err := app.Main(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
