# Recipe Compiler

## Purpose

The in-tree `recipe` package provides a declarative authoring layer over the existing `planner -> team -> host` runtime.

It compiles YAML or JSON into:

- `host.StartTeamRequest`
- `planner.Plan`

## Supported Authoring Primitives

- `task`
- `sequential`
- `parallel`
- `loop`
- `tool`

These are compile-time sugar only. They do not introduce a second runtime model.

## Step Modes

### `task`

Directly emits one `planner.TaskSpec`.

### `sequential`

Compiles child steps so each child depends on the previous child terminals.

### `parallel`

Compiles child steps with the same incoming dependencies and unions their terminal tasks.

### `loop`

Expands one task template over `for_each`.

Supported placeholders:

- `{{item}}`
- `{{index}}`

### `tool`

Compiles a tool-like orchestration step into a supervisor-owned task with `required_capabilities`.

## Example

```yaml
pattern: deepsearch
supervisor_profile: supervisor
worker_profiles: [researcher]
input:
  query: recipe example
flow:
  - mode: parallel
    steps:
      - task:
          id: branch-1
          kind: research
          input: architecture
          required_role: researcher
          writes: [branch.arch]
          publish: [shared, blackboard]
      - task:
          id: branch-2
          kind: research
          input: tooling
          required_role: researcher
          writes: [branch.tools]
          publish: [shared, blackboard]
  - task:
      id: synth
      kind: synthesize
      assignee_agent_id: supervisor
      reads: [branch.arch, branch.tools]
      publish: [shared]
```

CLI support:

```bash
hydaelyn validate --recipe recipe.yaml
hydaelyn compile --recipe recipe.yaml
```
