package assertions

import (
	"context"
	"fmt"
	"strings"

	"github.com/Viking602/go-hydaelyn/api"
	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/multiagent"
)

// Compile-time guarantees that every M4 multi-agent assertion satisfies
// eval.Assertion.
var (
	_ eval.Assertion = AgentInstanceSpawned{}
	_ eval.Assertion = SchedulerTookPath{}
	_ eval.Assertion = HandoffOccurred{}
	_ eval.Assertion = TeamTerminatedSuccessfully{}
	_ eval.Assertion = NoNonIdempotentToolAutoRetried{}
	_ eval.Assertion = BPBLikeMetric{}
)

// The multi-agent assertions observe the run through the transport vocabulary
// the multiagent layer records on the event store (multiagent/events.go):
// EventAgentInstanceCreated carries an instance's className, EventDispatchEmitted
// carries the scheduler's per-tick dispatch className, and EventTypedHandoff
// carries the handoff's from/to classNames. Each payload key mirrors the
// established action-attempt convention (EventActionAttemptStarted uses
// "toolName"): camelCase keys read straight off api.Event.Payload. The
// framework reads these through the public Runner.RunEvents façade, so an
// application that drives a Team need only append the events its scheduler emits.
//
// Execution boundary: these assertions grade a run that an application drove as
// a Team — they read the multiagent events that a scheduler/team run emits. The
// default single-agent scripted harness (eval.NewHarness) used by eval.Run does
// NOT spawn instances, dispatch a path, or emit typed handoffs, so a case that
// uses these assertions must supply a Harness whose run already drove a Team and
// recorded those events on the shared event store. Pair them with a custom
// Harness (or an externally driven run graded through Check directly), not with
// the default scripted Run path.

const (
	payloadClassName = "className"
	payloadFrom      = "from"
	payloadTo        = "to"
	payloadToolName  = "toolName"
)

// CountMatcher folds an observed count into a pass/fail verdict for
// AgentInstanceSpawned. The three constructors cover the spec's vocabulary:
// AtLeast(n), AtMost(n), and Exactly(n).
type CountMatcher struct {
	want int
	cmp  countComparison
	desc string
}

type countComparison int

const (
	countAtLeast countComparison = iota
	countAtMost
	countExactly
)

// AtLeast matches an observed count of n or more.
func AtLeast(n int) CountMatcher {
	return CountMatcher{want: n, cmp: countAtLeast, desc: fmt.Sprintf("at least %d", n)}
}

// AtMost matches an observed count of n or fewer.
func AtMost(n int) CountMatcher {
	return CountMatcher{want: n, cmp: countAtMost, desc: fmt.Sprintf("at most %d", n)}
}

// Exactly matches an observed count of exactly n.
func Exactly(n int) CountMatcher {
	return CountMatcher{want: n, cmp: countExactly, desc: fmt.Sprintf("exactly %d", n)}
}

// match reports whether got satisfies the matcher and describes the expectation.
func (m CountMatcher) match(got int) (bool, string) {
	switch m.cmp {
	case countAtMost:
		return got <= m.want, m.desc
	case countExactly:
		return got == m.want, m.desc
	default:
		return got >= m.want, m.desc
	}
}

// AgentInstanceSpawned asserts that one or more AgentInstances of a class were
// spawned during the run. With no CountMatcher the assertion requires at least
// one instance; a single matcher (AtLeast / AtMost / Exactly) bounds the count.
// Spawns are observed from EventAgentInstanceCreated events whose payload
// className matches.
type AgentInstanceSpawned struct {
	// ClassName is the AgentClass name whose instances are counted.
	ClassName string
	// Count, when set, bounds the number of spawned instances. The zero value
	// (no matcher) requires at least one.
	Count *CountMatcher
}

// AgentInstanceSpawnedWith builds an AgentInstanceSpawned for className bounded
// by an optional CountMatcher, mirroring the spec's variadic
// AgentInstanceSpawned(className, count ...CountMatcher) constructor. Passing no
// matcher requires at least one instance; the first matcher is honored and any
// extra are ignored.
func AgentInstanceSpawnedWith(className string, count ...CountMatcher) AgentInstanceSpawned {
	a := AgentInstanceSpawned{ClassName: className}
	if len(count) > 0 {
		m := count[0]
		a.Count = &m
	}
	return a
}

// Name returns the assertion's stable identifier.
func (a AgentInstanceSpawned) Name() string { return "AgentInstanceSpawned" }

// Check counts EventAgentInstanceCreated events for the class and grades the
// count against the matcher (or the implicit at-least-one default).
func (a AgentInstanceSpawned) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	events, err := runEvents(ctx, run, harness)
	if err != nil {
		return err
	}
	got := 0
	for _, ev := range events {
		if ev.Type != multiagent.EventAgentInstanceCreated {
			continue
		}
		if eventString(ev, payloadClassName) == a.ClassName {
			got++
		}
	}
	matcher := AtLeast(1)
	if a.Count != nil {
		matcher = *a.Count
	}
	if ok, desc := matcher.match(got); !ok {
		return fmt.Errorf("class %q spawned %d instance(s), want %s", a.ClassName, got, desc)
	}
	return nil
}

// SchedulerTookPath asserts that the scheduler dispatched work to a sequence of
// AgentClasses matching the given path. The observed path is the ordered list of
// className payloads on the run's EventDispatchEmitted events (the scheduler's
// per-tick dispatch decisions). The match is an exact, in-order sequence
// comparison, validating SequentialScheduler / RouterScheduler routing.
type SchedulerTookPath struct {
	// Path is the expected ordered sequence of dispatched AgentClass names.
	Path []string
}

// SchedulerTookPathOf builds a SchedulerTookPath from a variadic path, mirroring
// the spec's SchedulerTookPath(path ...string) constructor.
func SchedulerTookPathOf(path ...string) SchedulerTookPath {
	return SchedulerTookPath{Path: path}
}

// Name returns the assertion's stable identifier.
func (a SchedulerTookPath) Name() string { return "SchedulerTookPath" }

// Check compares the run's dispatch path against the expected path.
func (a SchedulerTookPath) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	events, err := runEvents(ctx, run, harness)
	if err != nil {
		return err
	}
	var got []string
	for _, ev := range events {
		if ev.Type != multiagent.EventDispatchEmitted {
			continue
		}
		if class := eventString(ev, payloadClassName); class != "" {
			got = append(got, class)
		}
	}
	if !equalStrings(got, a.Path) {
		return fmt.Errorf("scheduler took path %v, want %v", got, a.Path)
	}
	return nil
}

// HandoffOccurred asserts that at least one typed handoff moved work from a
// fromClass instance to a toClass instance during the run. Handoffs are observed
// from EventTypedHandoff events whose payload from/to classNames match.
type HandoffOccurred struct {
	// FromClass is the handoff source AgentClass name.
	FromClass string
	// ToClass is the handoff destination AgentClass name.
	ToClass string
}

// Name returns the assertion's stable identifier.
func (a HandoffOccurred) Name() string { return "HandoffOccurred" }

// Check scans the run's typed-handoff events for a matching from/to pair.
func (a HandoffOccurred) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	events, err := runEvents(ctx, run, harness)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.Type != multiagent.EventTypedHandoff {
			continue
		}
		if eventString(ev, payloadFrom) == a.FromClass && eventString(ev, payloadTo) == a.ToClass {
			return nil
		}
	}
	return fmt.Errorf("no typed handoff from %q to %q occurred", a.FromClass, a.ToClass)
}

// TeamTerminatedSuccessfully asserts that the team run reached a successful
// terminal status (RunStatusCompleted) rather than failing, being cancelled, or
// exhausting its budget mid-flight.
type TeamTerminatedSuccessfully struct{}

// Name returns the assertion's stable identifier.
func (a TeamTerminatedSuccessfully) Name() string { return "TeamTerminatedSuccessfully" }

// Check reports whether the run completed successfully.
func (a TeamTerminatedSuccessfully) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	if run.Status == api.RunStatusCompleted {
		return nil
	}
	return fmt.Errorf("team terminated with status %q, want %q", run.Status, api.RunStatusCompleted)
}

// NoNonIdempotentToolAutoRetried asserts that no non-idempotent tool was retried
// by the agent loop without going through the durable ActionAttempt protocol —
// enforcing ADR-015 §ToolSafety. A non-idempotent tool that legitimately retries
// does so under one ActionAttempt (one idempotency key, observed as a single
// EventActionAttemptStarted): a second ActionAttemptStarted carrying a fresh
// idempotency key for the same tool is the auto-retry the loop must never
// perform. The assertion flags any tool listed in NonIdempotentTools whose
// distinct started attempts exceed one. With NonIdempotentTools empty every tool
// is treated as non-idempotent (the conservative default).
type NoNonIdempotentToolAutoRetried struct {
	// NonIdempotentTools names the tools whose auto-retry is forbidden. Empty
	// treats every observed tool as non-idempotent.
	NonIdempotentTools []string
}

// Name returns the assertion's stable identifier.
func (a NoNonIdempotentToolAutoRetried) Name() string {
	return "NoNonIdempotentToolAutoRetried"
}

// Check inspects the run's ActionAttemptStarted events and reports a violation
// when a guarded tool started more than one distinct attempt.
func (a NoNonIdempotentToolAutoRetried) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	events, err := runEvents(ctx, run, harness)
	if err != nil {
		return err
	}
	guarded := make(map[string]bool, len(a.NonIdempotentTools))
	for _, name := range a.NonIdempotentTools {
		guarded[name] = true
	}
	// Count distinct started attempts per tool. A retried non-idempotent tool
	// surfaces as a second start for the same tool name.
	attempts := make(map[string]int)
	for _, ev := range events {
		if ev.Type != api.EventActionAttemptStarted {
			continue
		}
		tool := eventString(ev, payloadToolName)
		if tool == "" {
			continue
		}
		if len(guarded) > 0 && !guarded[tool] {
			continue
		}
		attempts[tool]++
	}
	for tool, n := range attempts {
		if n > 1 {
			return fmt.Errorf("non-idempotent tool %q was auto-retried (%d attempts started without approval)", tool, n)
		}
	}
	return nil
}

// BPBScorer is the application-supplied quality metric BPBLikeMetric grades. It
// computes a normalized quality score for an executed run (the
// autoresearch-borrowed "bits-per-byte"-like template); the framework supplies
// the executed run and the Harness it ran in, and BPBLikeMetric owns the
// pass/fail comparison against a threshold. Higher scores are better.
type BPBScorer interface {
	// Score returns the run's quality score, or an error if it cannot be
	// computed. The score is compared against BPBLikeMetric.Threshold.
	Score(ctx context.Context, run api.Run, harness eval.Harness) (float64, error)
}

// BPBLikeMetric grades an executed run with an application-supplied BPBScorer and
// passes when the score meets or exceeds Threshold. The framework owns only the
// comparator; the scorer owns what "quality" means for the domain.
type BPBLikeMetric struct {
	// Scorer computes the run's quality score.
	Scorer BPBScorer
	// Threshold is the inclusive minimum score the run must reach to pass.
	Threshold float64
}

// Name returns the assertion's stable identifier.
func (a BPBLikeMetric) Name() string { return "BPBLikeMetric" }

// Check runs the scorer and compares its score against Threshold.
func (a BPBLikeMetric) Check(ctx context.Context, run api.Run, harness eval.Harness) error {
	if a.Scorer == nil {
		return fmt.Errorf("BPBLikeMetric requires a scorer")
	}
	score, err := a.Scorer.Score(ctx, run, harness)
	if err != nil {
		return fmt.Errorf("score run: %w", err)
	}
	if score < a.Threshold {
		return fmt.Errorf("run scored %.4f, want at least %.4f", score, a.Threshold)
	}
	return nil
}

// runEvents reads the run's event stream through the public Runner façade.
func runEvents(ctx context.Context, run api.Run, harness eval.Harness) ([]api.Event, error) {
	runner := harness.Runner()
	if runner == nil {
		return nil, fmt.Errorf("harness returned a nil runner")
	}
	events, err := runner.RunEvents(ctx, run.ID)
	if err != nil {
		return nil, fmt.Errorf("read run events: %w", err)
	}
	return events, nil
}

// eventString reads a string payload field from an event, returning "" when the
// key is absent or not a string. Keys are trimmed so a payload built from a
// human-edited source does not silently miss.
func eventString(ev api.Event, key string) string {
	if ev.Payload == nil {
		return ""
	}
	s, _ := ev.Payload[key].(string)
	return strings.TrimSpace(s)
}

// equalStrings reports whether two string slices are equal element-by-element.
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
