package api

type Config struct {
	StoreProvider StoreProvider
	PolicyEngine  PolicyEngine
	OutputGateway OutputGateway
	Pipeline      PipelineComponents
}

func DefaultConfig() Config { return Config{} }
