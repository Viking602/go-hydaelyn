package research

import "github.com/Viking602/go-hydaelyn/internal/program"

// Type aliases for the program package, re-exported so external callers
// can provide a ProgramLoader without importing internal/.

type (
	ProgramDocument     = program.Document
	ProgramLoader       = program.Loader
	MemoryProgramLoader = program.MemoryLoader
	FSProgramLoader     = program.FSLoader
)
