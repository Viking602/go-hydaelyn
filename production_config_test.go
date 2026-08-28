package venat

import (
	"context"
	"errors"
	"testing"

	"github.com/Viking602/venat/api"
)

// recordingIntentAnalyzer proves a pipeline override survives config
// normalization: it is only reachable if NewProduction kept it in the resolved
// config it handed to the runtime.
type recordingIntentAnalyzer struct{ called bool }

func (a *recordingIntentAnalyzer) AnalyzeIntent(_ context.Context, run api.Run) (api.Intent, error) {
	a.called = true
	return api.Intent{Summary: run.Request}, nil
}

func TestNewProduction_RequiresHostSuppliedDependencies(t *testing.T) {
	tests := []struct {
		name string
		cfg  api.Config
	}{
		{name: "no dependencies", cfg: api.Config{}},
		{name: "store without policy", cfg: api.Config{StoreProvider: stubStoreProvider{}}},
		{name: "policy without store", cfg: api.Config{PolicyEngine: allowAPIEngine{}}},
		{
			name: "pipeline overrides do not stand in for the requirements",
			cfg:  api.Config{Pipeline: api.PipelineComponents{IntentAnalyzer: &recordingIntentAnalyzer{}}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewProduction(test.cfg); !errors.Is(err, api.ErrInvalidConfiguration) {
				t.Fatalf("NewProduction() error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestNewProduction_NormalizesConfigLikeNewDevelopment(t *testing.T) {
	ctx := context.Background()
	analyzer := &recordingIntentAnalyzer{}
	r, err := NewProduction(api.Config{
		StoreProvider: NewDevelopment().StoreProvider(),
		PolicyEngine:  denyOperationEngine{operation: api.PolicyOperationHandoff},
		Pipeline:      api.PipelineComponents{IntentAnalyzer: analyzer},
	})
	if err != nil {
		t.Fatalf("NewProduction() error = %v", err)
	}

	run, _, err := r.StartRun(ctx, api.StartRunCommand{Request: "normalize me"})
	if err != nil {
		t.Fatalf("StartRun() error = %v", err)
	}
	if _, err := r.AdvanceRun(ctx, api.AdvanceRunCommand{RunID: run.ID}); err != nil {
		t.Fatalf("AdvanceRun() error = %v", err)
	}
	if !analyzer.called {
		t.Fatal("NewProduction dropped the pipeline override during config normalization")
	}
	if err := requestHandoff(ctx, t, r, run.ID); !errors.Is(err, api.ErrPolicyDenied) {
		t.Fatalf("RequestHandoff() error = %v, want the configured engine to stay in force", err)
	}
}
