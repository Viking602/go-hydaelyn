// dataflow demonstrates blackboard read/write: producer task writes a finding,
// consumer task reads it via SelectItems. The blackboard is the framework's
// only shared-memory primitive.
//
//	go run ./_examples/dataflow
package main

import (
	"context"
	"fmt"

	"github.com/Viking602/go-hydaelyn/orchestrator"
)

func main() {
	ctx := context.Background()
	rt := orchestrator.NewRuntime(orchestrator.Config{})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "producer"})
	rt.RegisterAgent(orchestrator.AgentProfile{ID: "consumer"})

	run, _, err := rt.StartRun(ctx, orchestrator.StartRunCommand{Request: "share artifact"})
	must(err)

	// Producer publishes one Evidence item to the blackboard.
	must(rt.WriteItem(ctx, orchestrator.BlackboardItem{
		RunID:      run.ID,
		Type:       orchestrator.BlackboardItemEvidence,
		Source:     orchestrator.SourceIdentity{Type: orchestrator.SourceAgent, ID: "producer"},
		Content:    "p99 latency 320ms over the last hour",
		Visibility: orchestrator.BlackboardVisibilityAgentVisible,
	}))
	fmt.Println("producer wrote evidence")

	// Consumer queries by source agent and item type, prints what it sees.
	items, err := rt.SelectItems(ctx, run.ID, orchestrator.BlackboardSelector{
		ItemTypes:   []orchestrator.BlackboardItemType{orchestrator.BlackboardItemEvidence},
		SourceTypes: []orchestrator.SourceType{orchestrator.SourceAgent},
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
