package eval_test

import (
	"context"
	"testing"

	"github.com/Viking602/go-hydaelyn/eval"
	"github.com/Viking602/go-hydaelyn/eval/assert"
)

func TestEval_PassWhenAllAssertionsPass(t *testing.T) {
	c := eval.Case{
		Name: "greeting",
		Assertions: []eval.Assertion{
			assert.Contains{Needle: "hello"},
			assert.Regex{Pattern: `(?i)world`},
		},
	}
	out := eval.Eval(context.Background(), c, eval.Outcome{FinalText: "Hello, World"})
	if !out.Passed {
		t.Fatalf("expected pass, got %+v", out)
	}
	if len(out.Assertions) != 2 {
		t.Fatalf("expected 2 assertion results, got %d", len(out.Assertions))
	}
}

func TestEval_FailReportsDetail(t *testing.T) {
	c := eval.Case{
		Name:       "diff",
		Assertions: []eval.Assertion{assert.Equals{Want: "blue"}},
	}
	out := eval.Eval(context.Background(), c, eval.Outcome{FinalText: "red"})
	if out.Passed {
		t.Fatal("expected fail")
	}
	if out.Assertions[0].Detail == "" {
		t.Fatal("expected detail string")
	}
}

func TestRun_AggregatesSuite(t *testing.T) {
	s := eval.Suite{
		Name: "smoke",
		Cases: []eval.Case{
			{Name: "ok", Assertions: []eval.Assertion{assert.Contains{Needle: "a"}}},
			{Name: "fail", Assertions: []eval.Assertion{assert.Contains{Needle: "z"}}},
		},
	}
	out := eval.Run(context.Background(), s, func(ctx context.Context, c eval.Case) (eval.Outcome, error) {
		return eval.Outcome{FinalText: "abc"}, nil
	})
	if out.Passed {
		t.Fatal("expected suite to fail because of second case")
	}
	if got := out.SummaryLine(); got != "smoke: 1/2 cases passed (1 failed)" {
		t.Fatalf("summary = %q", got)
	}
	rows := out.FailedAssertions()
	if len(rows) != 1 || rows[0].CaseName != "fail" {
		t.Fatalf("failed rows = %+v", rows)
	}
}

func TestJudgedBy_HonorsCustomLogic(t *testing.T) {
	a := assert.JudgedBy{
		Label: "always-pass",
		Judge: func(ctx context.Context, o eval.Outcome) (bool, string) { return true, "judged ok" },
	}
	res := a.Check(context.Background(), eval.Outcome{})
	if !res.Passed {
		t.Fatal("expected pass")
	}
	if a.Name() != "judged:always-pass" {
		t.Fatalf("name = %q", a.Name())
	}
}
