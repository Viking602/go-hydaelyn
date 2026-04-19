package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/capability"
	"github.com/Viking602/go-hydaelyn/host"
)

func main() {
	runner := host.New(host.Config{})
	runner.RegisterCapability(capability.TypeSearch, "web", func(ctx context.Context, call capability.Call) (capability.Result, error) {
		return capability.Result{Output: map[string]any{
			"query": call.Name,
			"hits":  []string{"architecture", "tooling", "runtime"},
		}}, nil
	})

	result, err := runner.InvokeCapability(context.Background(), capability.Call{
		Type: capability.TypeSearch,
		Name: "web",
	})
	if err != nil {
		panic(err)
	}
	fmt.Printf("%v\n", result.Output)
}
