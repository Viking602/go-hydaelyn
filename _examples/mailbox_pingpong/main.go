// mailbox_pingpong demonstrates the agent-to-agent mailbox primitive: one
// agent sends a letter, another fetches it, acks it, and replies.
//
//	go run ./_examples/mailbox_pingpong
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/Viking602/go-hydaelyn/host"
	"github.com/Viking602/go-hydaelyn/mailbox"
	"github.com/Viking602/go-hydaelyn/storage"
	"github.com/Viking602/go-hydaelyn/team"
)

func main() {
	ctx := context.Background()
	driver := storage.NewMemoryDriver()
	runner := host.New(host.Config{Storage: driver})

	const teamRunID = "team-demo"
	// Seed a minimal run so fanout and Send have a team to address.
	err := driver.Teams().Save(ctx, team.RunState{
		ID:      teamRunID,
		Pattern: "demo",
		Status:  team.StatusRunning,
		Workers: []team.AgentInstance{
			{ID: "researcher", Role: team.RoleResearcher},
			{ID: "verifier", Role: team.RoleVerifier},
		},
	})
	if err != nil {
		log.Fatalf("Teams.Save: %v", err)
	}

	mbox := runner.Mailbox()

	// Step 1: researcher asks verifier to validate a claim.
	ids, err := mbox.Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "researcher"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "verifier"},
		Letter: mailbox.Letter{
			Subject: "verify claim",
			Body:    "Please confirm the alpha-cohort effect size is statistically significant.",
			Intent:  mailbox.IntentAsk,
		},
		CorrelationID: "thread-1",
	})
	if err != nil {
		log.Fatalf("Send (ask): %v", err)
	}
	fmt.Printf("ask delivered: %v\n", ids)

	// Step 2: verifier drains its inbox.
	envs, err := mbox.Fetch(ctx, teamRunID, "verifier", 8, 30*time.Second)
	if err != nil {
		log.Fatalf("Fetch: %v", err)
	}
	fmt.Printf("verifier fetched %d envelope(s)\n", len(envs))
	for _, env := range envs {
		fmt.Printf("  from=%s intent=%s body=%q\n", env.From.AgentID, env.Letter.Intent, env.Letter.Body)
		if err := mbox.Ack(ctx, mailbox.Receipt{
			EnvelopeID: env.ID,
			AgentID:    "verifier",
			AckedAt:    time.Now().UTC(),
			Outcome:    "handled",
		}); err != nil {
			log.Fatalf("Ack: %v", err)
		}
	}

	// Step 3: verifier replies. InReplyTo links the thread.
	if _, err := mbox.Send(ctx, mailbox.SendInput{
		TeamRunID: teamRunID,
		From:      mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "verifier"},
		To:        mailbox.Address{Kind: mailbox.AddressKindAgent, TeamRunID: teamRunID, AgentID: "researcher"},
		Letter: mailbox.Letter{
			Subject: "re: verify claim",
			Body:    "Confirmed. p=0.012, effect size d=0.41.",
			Intent:  mailbox.IntentAnswer,
		},
		CorrelationID: "thread-1",
		InReplyTo:     envs[0].ID,
	}); err != nil {
		log.Fatalf("Send (answer): %v", err)
	}

	// Step 4: researcher receives the answer.
	answers, err := mbox.Fetch(ctx, teamRunID, "researcher", 8, 30*time.Second)
	if err != nil {
		log.Fatalf("Fetch (answer): %v", err)
	}
	for _, env := range answers {
		fmt.Printf("researcher got answer: %q (inReplyTo=%s)\n", env.Letter.Body, env.InReplyTo)
		_ = mbox.Ack(ctx, mailbox.Receipt{EnvelopeID: env.ID, AgentID: "researcher", AckedAt: time.Now().UTC(), Outcome: "handled"})
	}

	// Show the full correlated thread.
	if mm, ok := mbox.(*mailbox.MemoryMailbox); ok {
		thread, err := mm.ListByCorrelation(ctx, teamRunID, "thread-1")
		if err != nil {
			log.Fatalf("ListByCorrelation: %v", err)
		}
		fmt.Printf("--- thread-1 (%d envelopes) ---\n", len(thread))
		for _, env := range thread {
			fmt.Printf("  %s: %s → %s [%s] state=%s\n",
				env.ID, env.From.AgentID, env.To.AgentID, env.Letter.Intent, env.State)
		}
	}
}
