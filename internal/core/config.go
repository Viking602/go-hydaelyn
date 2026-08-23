package core

import (
	"sync"
	"sync/atomic"

	"github.com/Viking602/venat/api"
	commandbus "github.com/Viking602/venat/internal/command"
	"github.com/Viking602/venat/internal/memory"
	storedel "github.com/Viking602/venat/internal/store"
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

	tools       map[string]api.Tool
	scopedTools map[toolHolderKey]map[string]api.Tool
	agents      map[string]AgentProfile
	agentOrder  []string
	flows       map[string]api.Flow

	policy         PolicyEngine
	policyEnforcer PolicyObligationEnforcer
	outputGateway  OutputGateway
	pipeline       PipelineComponents
}

type Config = api.Config

func NewMemoryRuntime() *Runtime {
	return NewRuntime(Config{})
}

func NewRuntime(config Config) *Runtime {
	rt := &Runtime{
		tools:          map[string]api.Tool{},
		scopedTools:    map[toolHolderKey]map[string]api.Tool{},
		agents:         map[string]AgentProfile{},
		agentOrder:     []string{},
		flows:          map[string]api.Flow{},
		policy:         allowPolicyEngine{},
		policyEnforcer: defaultPolicyObligationEnforcer{},
		outputGateway:  memoryOutputGateway{},
		memProvider:    memory.NewProvider(),
		commandBus:     commandbus.NewBus(),
	}
	if config.StoreProvider != nil {
		rt.storeProvider = config.StoreProvider
	}
	if config.PolicyEngine != nil {
		rt.policy = config.PolicyEngine
	}
	if config.PolicyEnforcer != nil {
		rt.policyEnforcer = config.PolicyEnforcer
	}
	if config.OutputGateway != nil {
		rt.outputGateway = config.OutputGateway
	}
	rt.Delegates = storedel.NewDelegates(storedel.Options{
		BeginWrite: rt.beginWriteUoW,
		BeginRead:  rt.beginReadUoW,
	})
	rt.pipeline = defaultPipeline(config.Pipeline)
	rt.registerUoWCommandHandlers()
	return rt
}
