package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hecate/agent-runtime/internal/sandbox"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "worker" {
		if err := sandbox.ServeWorker(context.Background(), os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	fmt.Fprintln(os.Stderr, "usage: sandboxd worker")
	os.Exit(2)
}
