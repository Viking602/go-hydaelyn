// Package coding is the reusable coding-agent toolkit: the coding.*
// tool.Drivers (read_file, search, edit_hashline, write_file, gofmt,
// gotest), the Workspace filesystem abstraction, and the hashline
// snapshot store behind stale-edit detection. It is runtime machinery,
// not a Pack.
//
// packs/coding is the deployable counterpart: a declarative manifest
// (AgentDefinition, CapabilityManifest, eval suite) that hosts mount and
// then bind to this package's drivers via coding.NewToolSet. The pack
// must never import this package — see the package comment in
// packs/coding for the boundary rationale and the mount recipe.
package coding
