package core

import pipelinesvc "github.com/Viking602/go-hydaelyn/internal/pipeline"

func defaultPipeline(config PipelineComponents) PipelineComponents {
	return pipelinesvc.Default(config)
}
