package main

import (
	"context"
	"fmt"
	"os"

	"github.com/Viking602/go-hydaelyn/internal/cli"
)

func main() {
	if err := cli.Execute(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
