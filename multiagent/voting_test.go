package multiagent

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Viking602/go-hydaelyn/agent"
)

func ballot(voter, option string) Ballot {
	return Ballot{Voter: voter, Result: agent.Result{Structured: json.RawMessage(`{"choice":"` + option + `"}`)}}
}

func TestMajorityVoteResolvesPlurality(t *testing.T) {
	got, err := MajorityVote("choice", ballot("a", "ship"), ballot("b", "ship"), ballot("c", "hold"))
	if err != nil {
		t.Fatalf("MajorityVote error = %v", err)
	}
	if got.Winner != "ship" {
		t.Fatalf("winner = %q, want ship", got.Winner)
	}
	if got.Tally["ship"] != 2 || got.Tally["hold"] != 1 {
		t.Fatalf("tally = %#v", got.Tally)
	}
	if got.Votes["a"] != "ship" || got.Votes["c"] != "hold" {
		t.Fatalf("votes = %#v", got.Votes)
	}
}

func TestMajorityVoteTieHasNoWinner(t *testing.T) {
	got, err := MajorityVote("choice", ballot("a", "x"), ballot("b", "y"))
	if err != nil {
		t.Fatalf("MajorityVote error = %v", err)
	}
	if got.Winner != "" {
		t.Fatalf("tie winner = %q, want empty", got.Winner)
	}
}

func TestMajorityVoteSkipsAbstentions(t *testing.T) {
	abstain := Ballot{Voter: "z"} // empty Structured
	got, err := MajorityVote("choice", ballot("a", "x"), abstain)
	if err != nil {
		t.Fatalf("MajorityVote error = %v", err)
	}
	if got.Winner != "x" || got.Tally["x"] != 1 || len(got.Votes) != 1 {
		t.Fatalf("result = %#v", got)
	}
}

func TestQuorumVoteRequiresThreshold(t *testing.T) {
	ballots := []Ballot{ballot("a", "go"), ballot("b", "go")}
	if got, _ := QuorumVote("choice", 3, ballots...); got.Winner != "" {
		t.Fatalf("winner below quorum = %q, want empty", got.Winner)
	}
	got, err := QuorumVote("choice", 2, ballots...)
	if err != nil {
		t.Fatalf("QuorumVote error = %v", err)
	}
	if got.Winner != "go" || got.Quorum != 2 {
		t.Fatalf("result = %#v", got)
	}
}

func TestQuorumVoteRejectsInvalidQuorum(t *testing.T) {
	if _, err := QuorumVote("choice", 0, ballot("a", "x")); !errors.Is(err, ErrInvalidQuorum) {
		t.Fatalf("error = %v, want ErrInvalidQuorum", err)
	}
}

func TestVoteRejectsInvalidStructuredJSON(t *testing.T) {
	bad := Ballot{Voter: "a", Result: agent.Result{Structured: json.RawMessage(`{`)}}
	if _, err := MajorityVote("choice", bad); err == nil {
		t.Fatal("expected error for invalid structured JSON")
	}
}
