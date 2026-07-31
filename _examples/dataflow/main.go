// dataflow demonstrates blackboard read/write: producer task writes a finding,
// consumer task reads it via SelectItems. The blackboard is the framework's
// only shared-memory primitive.
//
//	go run ./_examples/dataflow
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/venat"
	"github.com/Viking602/venat/api"
)

func main() {
	ctx := context.Background()
	runner := venat.NewDevelopment()
	runner.RegisterAgent(api.AgentProfile{ID: "producer"})
	runner.RegisterAgent(api.AgentProfile{ID: "consumer"})

	run, _, err := runner.StartRun(ctx, api.StartRunCommand{Request: "share artifact"})
	must(err)

	// Producer publishes one Evidence item to the blackboard.
	must(runner.WriteItem(ctx, api.BlackboardItem{
		RunID:      run.ID,
		Type:       api.BlackboardItemEvidence,
		Source:     api.SourceIdentity{Type: api.SourceAgent, ID: "producer"},
		Content:    "p99 latency 320ms over the last hour",
		Visibility: api.BlackboardVisibilityAgentVisible,
	}))
	fmt.Println("producer wrote evidence")

	// Consumer queries by source agent and item type, prints what it sees.
	items, err := runner.SelectItems(ctx, run.ID, api.BlackboardSelector{
		ItemTypes:   []api.BlackboardItemType{api.BlackboardItemEvidence},
		SourceTypes: []api.SourceType{api.SourceAgent},
		SourceIDs:   []string{"producer"},
	})
	must(err)
	fmt.Printf("consumer read %d item(s):\n", len(items))
	for _, it := range items {
		fmt.Printf("  [%s] %s — %s\n", it.Type, it.Source.ID, it.Content)
	}
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}
