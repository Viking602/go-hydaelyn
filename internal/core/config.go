package core

import (
	"sync"
	"sync/atomic"

	commandbus "github.com/Viking602/go-hydaelyn/internal/command"
	"github.com/Viking602/go-hydaelyn/internal/core/model"
	"github.com/Viking602/go-hydaelyn/internal/memory"
	storedel "github.com/Viking602/go-hydaelyn/internal/store"
)

const maxHandoffDepth = 8

type Runtime struct {
	configMu sync.RWMutex // guards policy, outputGateway, pipeline
	mu       sync.Mutex   // guards agents, tools, flows
	idSeq    atomic.Int64

	memProvider   *memory.Provider
	storeProvider StoreProvider // non-nil only for external Config.StoreProvider
	commandBus    *commandbus.Bus
	*storedel.Delegates

	tools      map[string]model.Tool
	agents     map[string]AgentProfile
	agentOrder []string
	flows      map[string]model.Flow

	policy        PolicyEngine
	outputGateway OutputGateway
	pipeline      PipelineComponents
}

type Config struct {
	StoreProvider StoreProvider
	PolicyEngine  PolicyEngine
	OutputGateway OutputGateway
	Pipeline      PipelineComponents
}

func NewMemoryRuntime() *Runtime {
	return NewRuntime(Config{})
}

func NewRuntime(config Config) *Runtime {
	rt := &Runtime{
		tools:         map[string]model.Tool{},
		agents:        map[string]AgentProfile{},
		agentOrder:    []string{},
		flows:         map[string]model.Flow{},
		policy:        allowPolicyEngine{},
		outputGateway: memoryOutputGateway{},
		memProvider:   memory.NewProvider(),
		commandBus:    commandbus.NewBus(),
	}
	if config.StoreProvider != nil {
		rt.storeProvider = config.StoreProvider
	}
	if config.PolicyEngine != nil {
		rt.policy = config.PolicyEngine
	}
	if config.OutputGateway != nil {
		rt.outputGateway = config.OutputGateway
	}
	rt.Delegates = storedel.NewDelegates(storedel.Options{
		BeginWrite: rt.beginWriteUoW,
		BeginRead:  rt.beginReadUoW,
		ResumeTokens: func() map[string]model.ResumeToken {
			snap := rt.memProvider.CommittedSnapshot()
			result := make(map[string]model.ResumeToken, len(snap.ResumeTokens))
			for k, v := range snap.ResumeTokens {
				result[k] = v
			}
			return result
		},
	})
	rt.pipeline = defaultPipeline(config.Pipeline)
	rt.registerUoWCommandHandlers()
	return rt
}
