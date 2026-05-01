// dataflow demonstrates blackboard read/write: producer task writes a finding,
// consumer task reads it via SelectItems. The blackboard is the framework's
// only shared-memory primitive.
//
//	go run ./_examples/dataflow
package main

import (
	"context"
	"fmt"

	hydaelyn "github.com/Viking602/go-hydaelyn"
)

func main() {
	ctx := context.Background()
	runner := hydaelyn.New()
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "producer"})
	runner.RegisterAgent(hydaelyn.AgentProfile{ID: "consumer"})

	run, _, err := runner.StartRun(ctx, hydaelyn.StartRunCommand{Request: "share artifact"})
	must(err)

	// Producer publishes one Evidence item to the blackboard.
	must(runner.WriteItem(ctx, hydaelyn.BlackboardItem{
		RunID:      run.ID,
		Type:       hydaelyn.BlackboardItemEvidence,
		Source:     hydaelyn.SourceIdentity{Type: hydaelyn.SourceAgent, ID: "producer"},
		Content:    "p99 latency 320ms over the last hour",
		Visibility: hydaelyn.BlackboardVisibilityAgentVisible,
	}))
	fmt.Println("producer wrote evidence")

	// Consumer queries by source agent and item type, prints what it sees.
	items, err := runner.SelectItems(ctx, run.ID, hydaelyn.BlackboardSelector{
		ItemTypes:   []hydaelyn.BlackboardItemType{hydaelyn.BlackboardItemEvidence},
		SourceTypes: []hydaelyn.SourceType{hydaelyn.SourceAgent},
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
