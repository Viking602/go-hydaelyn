package multiagent

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/Viking602/venat/agent"
)

// VotingResult aggregates votes cast by AgentInstances on a candidate
// discriminator field. MajorityVote / QuorumVote populate it; the
// vote-collection scheduler (DebateScheduler) still lands in v0.9.0.
//
// Candidate is the discriminator field voted on; Votes maps each voter to
// the option it chose; Tally is the count per option; Quorum is the
// minimum vote count for the result to count as resolved (0 for a simple
// majority).
type VotingResult struct {
	Candidate string            `json:"candidate"`
	Votes     map[string]string `json:"votes,omitempty"`
	Tally     map[string]int    `json:"tally,omitempty"`
	Winner    string            `json:"winner,omitempty"`
	Quorum    int               `json:"quorum,omitempty"`
}

// ErrInvalidQuorum is returned by QuorumVote when quorum is below 1.
var ErrInvalidQuorum = errors.New("multiagent: quorum must be >= 1")

// Ballot pairs a voter identity (typically an AgentInstance ID) with the
// agent.Result it produced. Voting reads the chosen option from a
// discriminator field in the result's Structured payload.
type Ballot struct {
	Voter  string
	Result agent.Result
}

// MajorityVote tallies ballots by the value of field in each result's
// Structured payload and resolves the plurality winner. Ballots that omit
// the field (or carry empty Structured) abstain. A tie for the top tally
// yields an empty Winner. Invalid Structured JSON is an error.
func MajorityVote(field string, ballots ...Ballot) (VotingResult, error) {
	result, err := tallyBallots(field, 0, ballots)
	if err != nil {
		return VotingResult{}, err
	}
	result.Winner = topVote(result.Tally, 1)
	return result, nil
}

// QuorumVote is MajorityVote that additionally requires the winning option
// to reach quorum votes; when no option does, Winner is empty.
func QuorumVote(field string, quorum int, ballots ...Ballot) (VotingResult, error) {
	if quorum < 1 {
		return VotingResult{}, ErrInvalidQuorum
	}
	result, err := tallyBallots(field, quorum, ballots)
	if err != nil {
		return VotingResult{}, err
	}
	result.Winner = topVote(result.Tally, quorum)
	return result, nil
}

func tallyBallots(field string, quorum int, ballots []Ballot) (VotingResult, error) {
	result := VotingResult{
		Candidate: field,
		Votes:     map[string]string{},
		Tally:     map[string]int{},
		Quorum:    quorum,
	}
	for _, ballot := range ballots {
		option, err := ballotOption(ballot.Result, field)
		if err != nil {
			return VotingResult{}, fmt.Errorf("multiagent: ballot %q: %w", ballot.Voter, err)
		}
		if option == "" {
			continue
		}
		result.Votes[ballot.Voter] = option
		result.Tally[option]++
	}
	return result, nil
}

func ballotOption(result agent.Result, field string) (string, error) {
	if len(result.Structured) == 0 {
		return "", nil
	}
	var structured map[string]any
	if err := json.Unmarshal(result.Structured, &structured); err != nil {
		return "", err
	}
	return discriminatorValue(structured, field), nil
}

// topVote returns the option with the strictly-highest tally that also
// reaches threshold votes. An empty tally set, a top tally below
// threshold, or a tie for the top tally each return "".
func topVote(tally map[string]int, threshold int) string {
	type optionTally struct {
		option string
		count  int
	}
	ranked := make([]optionTally, 0, len(tally))
	for option, count := range tally {
		ranked = append(ranked, optionTally{option: option, count: count})
	}
	if len(ranked) == 0 {
		return ""
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].count != ranked[j].count {
			return ranked[i].count > ranked[j].count
		}
		return ranked[i].option < ranked[j].option
	})
	top := ranked[0]
	if top.count < threshold {
		return ""
	}
	if len(ranked) > 1 && ranked[1].count == top.count {
		return ""
	}
	return top.option
}
