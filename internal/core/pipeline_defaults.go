package core

import pipelinesvc "github.com/Viking602/venat/internal/pipeline"

func defaultPipeline(config PipelineComponents) PipelineComponents {
	return pipelinesvc.Default(config)
}
