# Coding Agent + Hashline Editing — Implementation Spec (v2)

Status: Approved for implementation (2026-06-02). Supersedes the v1 draft.

This document specifies a sandboxed **coding capability** for Hydaelyn plus a
**pure-Go implementation of the hashline line-anchored edit protocol**. It is the
single source of truth for the implementation; every package, type, and behavior
below is decision-complete.

## 0. Hard constraints

- **Pure Go, no other languages.** No cgo, no JavaScript interop, no third-party
  hashing library, no tree-sitter. The tag hash uses the standard library
  (`hash/fnv`). Formatting uses `go/format` in-process. The only subprocesses are
  `go test` and `git diff` (the Go toolchain and VCS themselves, invoked without a
  shell, through a strict allowlist).
- **No new third-party dependencies.** Do not modify `go.mod` require directives.
  Standard library only.
- **Tabs** in Go source; goimports local prefix `github.com/Viking602/go-hydaelyn`.
- Typed result structs only in the public `coding/` surface — never return `[]any`
  or expose loose `any` fields (house style; `coding/` is outside the ADR-009
  enforcement set but follows the same rule).
- Do **not** `git commit`/`push`. Leave changes in the working tree for review.

## 1. Architecture & layering

```text
model
  → tool.Bus                 (Subset: only the tools the agent declares)
  → worker.GovernedToolBus    → Runner.InvokeTool → toolgate (Task.AllowsAction)
                                                   → coding.PolicyEngine
  → coding/* tool.Driver      (read_file/search/edit_hashline/write_file/gofmt/go_test/git_diff)
  → coding.Workspace          (path + command sandbox; implements hashline.Filesystem)
  → coding/internal/hashline  (parse → preflight → all-or-nothing commit; tag = hash(live file))
  → [M6] SnapshotStore history (only 3-way recovery needs it; deferred)
```

Boundaries (unchanged from v1):

| Layer | Responsibility | Does NOT |
|---|---|---|
| `agent.Engine` | drive model + tool calls | touch the filesystem |
| `tool.Bus` | register/dispatch tools | make policy decisions |
| `worker.GovernedToolBus` | bind invocation to run/task/lease/policy | implement edit logic |
| `coding/*` tools | coding tools + workspace | change the Runner kernel |
| `coding/internal/hashline` | parse/validate/apply hashline patches | touch real disk or know about agents |
| `coding.Workspace` | sandboxed FS + command exec | accept raw shell |

## 2. Package layout (decision-complete)

`coding/` is a **new top-level runtime package**, a sibling of `provider/`,
`tool/`, `policy/`, `memory/`. Tool implementations live here. `packs/coding/` is a
thin **declarative manifest only** (per `packs/packs.go`: packs must not touch the
kernel; the host binds capabilities to implementations).

```text
coding/
  coding.go            // façade: NewToolSet(ws) []tool.Driver, PolicyEngine(), AgentClass(), Instructions
  workspace.go         // Workspace interface + NewLocalWorkspace(root) (implements hashline.Filesystem)
  read_file.go         // coding.read_file driver
  search.go            // coding.search driver
  edit_hashline.go     // coding.edit_hashline driver
  write_file.go        // coding.write_file driver (create-new-file only)
  gofmt.go             // coding.gofmt driver (in-process go/format)
  gotest.go            // coding.go_test driver (allowlisted subprocess). NOTE: the file is named
                       //   gotest.go, NOT go_test.go — Go excludes *_test.go files from the build.
                       //   The tool name is still exactly "coding.go_test".
  git_diff.go          // coding.git_diff driver
  *_test.go

  internal/hashline/
    normalize.go hash.go format.go
    parser.go apply.go patcher.go
    snapshot.go          // interface now; MemorySnapshotStore impl deferred to M6
    errors.go
    *_test.go

  internal/workspace/
    path.go command.go glob.go
    *_test.go

packs/coding/
  coding.go            // single file: api.AgentDefinition + api.CapabilityManifest + []eval.EvalCase
```

Import rule: `packs/coding` imports only the declarative-only set the worked
example `packs/research/research.go` uses — `api`, `eval`, `eval/assertions`,
`packs`, and `provider` (the last two for the `packs.Pack` value and the scripted
smoke harness). It must NOT import `coding/` (the runtime) or the kernel. The
wiring (build a `tool.Bus` from `coding.NewToolSet(ws)` and attach to the agent)
is done by the host / example.

Source files to read before implementing (verify exact signatures against these):
`tool/tool.go`, `tool/kit/toolkit.go`, `message/message.go`, `policy/policy.go`,
`worker/tools.go`, `multiagent/class.go`, `packs/packs.go`, `packs/devops/devops.go`,
`api/agent_definition.go`, `eval/case.go`, `eval/runner.go`, `eval/assertions/`.

## 3. Hashline protocol

### 3.1 Read/search output

`coding.read_file` returns hashline-grounded numbered lines:

```text
¶internal/foo.go#A1B2
1:package internal
2:
3:func Add(a, b int) int {
4:    return a + b
5:}
```

- `¶` section prefix, then workspace-relative path, then `#TAG` (4 uppercase hex).
- `N:` is the 1-based line number; the text after `:` is the verbatim line.

Structured result (typed Go struct, marshaled to the tool Result):

```json
{"path":"internal/foo.go","tag":"A1B2","header":"¶internal/foo.go#A1B2",
 "startLine":1,"endLine":5,"content":"¶internal/foo.go#A1B2\n1:package internal\n..."}
```

Rules:
- The tag is computed from the **full normalized file**, even when only a slice is
  returned. `read_file` and `search` both mint tags this way.
- Same path + same content ⇒ same tag. File changes ⇒ new tag.
- No persistent store is required to mint/validate: tag = `ComputeFileHash(live file)`.

### 3.2 Edit input grammar

First release supports these operations (line numbers are 1-based, relative to the
**original** file the tag refers to):

```text
¶PATH#TAG
replace N..M:
+new line 1
+new line 2

delete N..M

insert before N:
+new line

insert after N:
+new line

insert head:
+first line

insert tail:
+last line
```

Multi-file: concatenate sections, each starting with its own `¶PATH#TAG`. Body rows
are final content only and must start with `+`. No `-old` rows, no bare context rows.

### 3.3 Edit output

```text
¶internal/foo.go#E7F8
updated internal/foo.go
firstChangedLine: 4

--- compact diff ---
-    return a-b
+    return a + b
```

Structured result: `{sections:[{path,op,oldTag,newTag,header,firstChangedLine,diff,warnings}]}`.
After a successful edit the old tag and old line numbers are dead; the agent must use
the new header or re-read before the next edit.

## 4. Hashline Go core (`coding/internal/hashline`)

### 4.1 Types

```go
type Snapshot struct {
    Path       string
    Text       string // LF-normalized, BOM-stripped
    Hash       string // 4-hex uppercase
    RecordedAt time.Time
}

type SnapshotStore interface { // M1/M2 may use a no-op/lazy impl; history impl is M6
    Head(path string) (Snapshot, bool)
    ByHash(path, hash string) (Snapshot, bool)       // latest version whose tag == hash
    UniqueByHash(path, hash string) (Snapshot, bool) // sole content for the tag, or false if 0 or ambiguous
    Record(path, fullText string) string
    Invalidate(path string)
    Clear()
}

type Patch struct{ Sections []Section }
type Section struct {
    Path string
    Tag  string
    Ops  []Op
}
type OpKind string
const (
    OpReplace      OpKind = "replace"
    OpDelete       OpKind = "delete"
    OpInsertBefore OpKind = "insert_before"
    OpInsertAfter  OpKind = "insert_after"
    OpInsertHead   OpKind = "insert_head"
    OpInsertTail   OpKind = "insert_tail"
)
type Op struct {
    Kind  OpKind
    Start int
    End   int
    Body  []string
}
```

### 4.2 normalize.go

```go
type NormalizedFile struct {
    Raw        string
    BOM        string // "" or "﻿"
    LineEnding string // "\n" or "\r\n"
    Text       string // normalized: LF, no BOM
}
```

- Detect & strip a leading UTF-8 BOM.
- Detect line ending: if any `\r\n` present treat file as CRLF, else LF.
- Internal `Text` is always LF with no BOM.
- On commit, restore the original BOM and line ending.

### 4.3 hash.go — pure Go, `hash/fnv`

```text
NormalizeForHash(text):
  for each line, trim trailing [ \t\r] before the newline or EOF
ComputeFileHash(text):
  n := NormalizeForHash(text)
  h := fnv.New32a(); h.Write([]byte(n))
  return strings.ToUpper(fmt.Sprintf("%04X", uint16(h.Sum32()&0xFFFF)))
```

Document verbatim: `Hashline syntax-compatible; tag is a Go-internal FNV fingerprint,
not cross-language compatible.` The 4-hex value is only the model-facing handle.
Because it is just the low 16 bits, two different file versions can share a tag, so
the patcher does not trust it alone: every read/search/edit records the content its
tag was minted from (§4.8) and the store retains colliding versions distinctly
(§4.8), so when any history is recorded for the path under that tag,
`Patcher.Preflight` takes the fast path only against an *unambiguous* base — the tag
must pin to a single recorded content (`SnapshotStore.UniqueByHash`) that equals the
live file. A live file that shares the tag but was never recorded, or a tag that
maps to two distinct recorded versions (a genuine 16-bit collision, which would make
the edit's line numbers ambiguous), is rejected as stale and forces a re-read. The
tag is the cheap pre-check; unambiguous-base resolution is the backstop that stops a
16-bit collision from applying a stale patch.

### 4.4 format.go

`FormatHeader(path, tag) string` → `¶PATH#TAG`.
`FormatNumberedLine(n int, text string) string` → `N:TEXT`.
`FormatNumberedLines(text string, startLine int) string` → joined numbered lines.

### 4.5 parser.go — strict

Reject and return a precise line-numbered, sentinel-typed error for: missing header,
header not `¶PATH#TAG`, empty path, tag not `[0-9A-F]{4}`, unknown operation,
`replace`/`insert` body row not starting with `+`, `delete` carrying a body, line
number `< 1`, range `start > end`, bare context line, `-old` row, unknown line.

```go
var (
    ErrParse            = errors.New("hashline: parse error")
    ErrMissingHeader    = errors.New("hashline: missing section header")
    ErrMissingTag       = errors.New("hashline: missing snapshot tag")
    ErrInvalidTag       = errors.New("hashline: invalid snapshot tag")
    ErrInvalidOperation = errors.New("hashline: invalid operation")
    ErrInvalidBodyRow   = errors.New("hashline: invalid body row")
    ErrSnapshotMismatch = errors.New("hashline: snapshot tag does not match live file")
    ErrNoop             = errors.New("hashline: edit is a no-op")
)
```

### 4.6 apply.go

```go
func Apply(text string, sec Section) (ApplyResult, error)
type ApplyResult struct {
    Text             string
    FirstChangedLine int
    Warnings         []string
}
```

- All op line numbers reference the **original** file; an earlier op must not shift a
  later op's indices.
- Validate: replace/delete/insert-before/insert-after ranges in-bounds; insert
  head/tail always allowed.
- Reject overlapping ranges and two ops touching the same original line.
- Strategy: split into `[]string`; lower ops to edit events keyed by original index;
  detect conflicts/out-of-bounds; rebuild; compute first changed line; if result equals
  input, return `ErrNoop` (or a noop warning per caller).

### 4.7 patcher.go — all-or-nothing

```go
type Filesystem interface {
    CanonicalPath(path string) (string, error)
    ReadText(ctx context.Context, path string) (string, error)
    PreflightWrite(ctx context.Context, path string) error
    WriteText(ctx context.Context, path, text string) error
}
type Patcher struct {
    FS        Filesystem
    Snapshots SnapshotStore // optional; nil-safe for stale-reject path
}
func (p *Patcher) Preflight(ctx context.Context, patch Patch) ([]PreparedSection, error)
func (p *Patcher) Commit(ctx context.Context, prepared []PreparedSection) (ApplyPatchResult, error)
func (p *Patcher) Apply(ctx context.Context, patch Patch) (ApplyPatchResult, error)
```

`Apply` sequence (no persistent store needed):

```text
1 parse already done (Patch passed in)
2 each section: CanonicalPath validation
3 each section: ReadText live file → ComputeFileHash → compare to section.Tag;
                 mismatch ⇒ ErrSnapshotMismatch (stale-reject)
4 each section: Apply in memory; keep new text + original text (rollback buffer)
5 only after ALL sections succeed: PreflightWrite each, then WriteText each
6 if any WriteText fails: restore already-written files from the rollback buffer
                 (via the uncapped restore path so an original above the forward
                 write cap is still put back); fail
7 build per-section result: new header (recompute tag of new text), compact diff, firstChangedLine
```

Stale handling, first release: hash match ⇒ apply; mismatch ⇒ `ErrSnapshotMismatch`
with a message instructing the agent to re-read. (3-way recovery is M6.)

Duplicate-section guard: two sections that target the same file are rejected up
front (each is preflighted against the original content, so a second `Commit` write
would clobber the first's edit — fold the ops into one `¶PATH#TAG` section instead).
The guard keys on the file's resolved on-disk identity when the `Filesystem`
implements the optional `identityResolver` (`ResolveIdentity(path) (string, error)`,
returning the symlink-resolved absolute path), so two in-root symlink aliases for
the same file (e.g. `a.go` and `link.go → a.go`) collapse to one key; without that
capability it falls back to the canonical path. (Rollback uses the same optional-
capability pattern via `restorer`, see step 6.)

### 4.8 snapshot.go

Define the `SnapshotStore` interface now. For M1–M5 a lazy/no-op implementation is
sufficient (stale-reject reads live files). `MemorySnapshotStore` with bounded
per-path history (`maxPaths=64`, `maxVersionsPerPath=8`, LRU) is implemented in M6 to
back 3-way recovery and `ByHash`. Versions are keyed by exact content, not by tag:
recording identical content deduplicates, but two distinct contents that collide on
the 16-bit tag are retained side by side (the index maps a tag to every version
sharing it, newest last). `ByHash` returns the newest version for a tag, while
`UniqueByHash` returns a version only when it is the *sole* content recorded under
the tag — reporting failure both when nothing is recorded and when ≥2 distinct
contents collide. The fast-path and recovery base lookups use `UniqueByHash`, so an
ambiguous (collided) tag is treated as unidentifiable and rejected rather than
applied against an arbitrarily-chosen colliding version.

## 5. Workspace sandbox (`coding/internal/workspace` + `coding/workspace.go`)

### 5.1 Interface

```go
type Workspace interface {
    Root() string
    ListFiles(ctx context.Context, req ListFilesRequest) (ListFilesResult, error)
    ReadFile(ctx context.Context, req ReadFileRequest) (ReadFileResult, error)
    Search(ctx context.Context, req SearchRequest) (SearchResult, error)
    WriteFile(ctx context.Context, req WriteFileRequest) (WriteFileResult, error)
    RunCommand(ctx context.Context, req RunCommandRequest) (RunCommandResult, error)
    Diff(ctx context.Context, req DiffRequest) (DiffResult, error)
}
```

`NewLocalWorkspace(root)` returns a `Workspace` that also satisfies
`hashline.Filesystem`.

### 5.2 Path safety — `ResolveWorkspacePath(root, rel) (abs, canonicalRel string, err error)`

```text
clean := filepath.Clean(rel)
reject if rel empty, filepath.IsAbs(clean), clean == ".." or HasPrefix(clean, "../"),
       or rel contains a NUL byte
abs := filepath.Join(root, clean)
// Symlink check must cover not-yet-created files: resolve the deepest EXISTING
// ancestor (the parent dir for a new file), not just the leaf.
resolvedAncestor := EvalSymlinks(deepest existing ancestor of abs)
reject if resolvedAncestor escapes EvalSymlinks(root)
deny .git/** by default; deny obvious binary/large files for read/search
// The .git deny is also enforced on the CANONICAL target, not just the lexical
// path: a symlink alias such as "g -> .git" has a benign cleaned path ("g/config")
// yet resolves inside the in-bounds .git tree, so reject when the resolved target
// lands under EvalSymlinks(root/.git).
reject if resolvedAncestor is within EvalSymlinks(root/.git)
```

### 5.3 Command safety

Allowlist only (matched on argv, never via a shell):

```text
go test ./...        go test ./<pkg>...     go test ./<pkg> -run <Name>
go vet ./...         git [-c core.fsmonitor=false] status --short
git [-c core.fsmonitor=false] diff [--no-ext-diff] [--no-textconv] -- <paths>   (helpers off; see §6.6)
```

The only `-c key=value` override the allowlist accepts is the exact pair
`-c core.fsmonitor=false`, and only as a leading global flag before the
subcommand — see §6.6 for why.

`gofmt` is NOT a subprocess — see §6.5. Forbid `sh -c`, `bash -c`, `curl`, `wget`,
`rm`, `python`, `node`, `npm`, `bun`, `git commit`, `git push`.

```go
type RunCommandRequest struct {
    Args           []string
    WorkingDir     string
    Timeout        time.Duration // default 30s
    MaxOutputBytes int           // default 64 KiB
    Env            map[string]string
}
```

Use `exec.CommandContext` with explicit argv. Scrub env (no token/secret passthrough).
Truncate stdout/stderr at the cap and mark `truncated`.

## 6. Tools (`coding/`)

Build each as a `tool.Driver` (`Definition() tool.Definition` + `Execute(ctx, call,
sink) (tool.Result, error)`). Prefer the `tool/kit` builder if its constructor fits;
otherwise implement `tool.Driver` directly. Metadata uses the real fields on
`message.ToolDefinition` (`EffectType`, `RequiresActionTask`, `RiskLevel`,
`PolicyTags`, `InputSchema`).

| Tool | EffectType | RequiresActionTask | RiskLevel | PolicyTags |
|---|---|---|---|---|
| `coding.list_files` | read_only | false | low | coding, read |
| `coding.read_file` | read_only | false | low | coding, read |
| `coding.search` | read_only | false | low | coding, search |
| `coding.git_diff` | read_only | false | low | coding, git, diff |
| `coding.edit_hashline` | write | true | medium | coding, edit, hashline, workspace-write |
| `coding.write_file` | write | true | medium | coding, create-file |
| `coding.gofmt` | write | true | low | coding, format |
| `coding.go_test` | read_only | false | medium | coding, test, run |

### 6.1 read_file
1 validate path → 2 read full file → 3 normalize → 4 `tag = ComputeFileHash` →
5 slice requested `[start_line,end_line]` (default whole file, enforce `max_bytes`) →
6 return `¶PATH#TAG` + numbered lines + structured result.

### 6.2 search
1 validate query (substring or regexp) → 2 walk workspace text files (respect ignores,
`.git` denylist, max results/bytes) → 3 for each matched file compute tag from full
normalized content → 4 return grouped `¶PATH#TAG` sections with context lines.

### 6.3 edit_hashline
Input schema: `{input: string (required), dry_run: bool}`. Behavior: parse JSON →
parse hashline `input` → validate paths → `Patcher.Preflight` → if `dry_run` return
diff preview only → `Patcher.Commit` → return new headers + compact diff. On
`ErrSnapshotMismatch`/parse error, return an error message that instructs the agent to
re-read before retrying. Emit an audit event (see §7) via `sink`.

### 6.4 write_file (create-new-file only)
If the file exists → reject and tell the agent to use `coding.edit_hashline`. Else
validate path, enforce max size, write, return `¶PATH#TAG`.

### 6.5 gofmt — in-process `go/format`
Read file → `format.Source([]byte)` → if changed, write back, return the diff. Pure
Go, no subprocess, no goimports (documented limitation; import management is out of
scope for v1). Reject non-`.go` and out-of-workspace paths.

### 6.6 go_test / git_diff
Thin wrappers over `Workspace.RunCommand` with the allowlist in §5.3. `go_test` is
classified `read_only` for its file effect (it mutates no workspace files; the
test cache/temp files stay under the toolchain's own directories) but `go test`
compiles and *executes* the workspace's own code, so it carries the `run` tag and
`coding.PolicyEngine()` escalates it to `EffectRequireApproval` (§7.1) — execution
goes behind the same explicit-allowance gate as the writes, not the free read
path. `git_diff` runs with `-c core.fsmonitor=false --no-ext-diff --no-textconv`:
`--no-ext-diff`/`--no-textconv` neutralize a repo-local `diff.external`/textconv
helper, and `-c core.fsmonitor=false` disables any configured filesystem-monitor
hook that git would otherwise execute while refreshing the index — without it, a
hostile `.git/config` pointing `core.fsmonitor` at a script could turn this
read-only diff into command execution. It returns bounded output. The allowlist
admits that single `-c` override (only the exact `core.fsmonitor=false` value)
and nothing else.

## 7. Policy & audit

### 7.1 `coding.PolicyEngine() policy.Engine`
A `policy.EngineFunc` composed over `policy.DenySideEffectsByDefault()`:
read-only coding tools allowed; `delete`/`run`-tagged tools ⇒ `EffectRequireApproval`.
The public `policy/` package does not evaluate `RiskLevel`/`PolicyTags` itself, so this
engine inspects `request.Tool.PolicyTags`. Ship governance tests (read allowed by
default; edit denied without action/allowance; denied edit leaves the workspace
unchanged).

IMPORTANT: `DenySideEffectsByDefault()` denies **every** write/`RequiresActionTask`
tool unconditionally — and the toolgate checks `Task.AllowsAction` *before* the policy,
so an action task alone does NOT clear the deny. To actually run `coding.edit_hashline`
or `coding.gofmt`, the host must compose `coding.PolicyEngine()` with an explicit
allowance (by tool name or PolicyTag) for those writes on the authorized action task —
this is the "explicit allowance clears the default deny" pattern, demonstrated in
`_examples/coding_hashline`.

### 7.2 Audit
Do **not** add fields to `api.ActionAttempt` (its real fields: AttemptID, ActionID,
RunID, TaskID, ToolName, Status, IdempotencyKey, InputHash, ExternalRequestID,
ExternalResultRef, RequiresReconcile). `InputHash` already covers the attempted patch.
Surface edit metadata (`oldTags`, `newTags`, `firstChangedLines`, `diffHash`) as the
typed tool `Result` and emit it to the run event stream via the tool's `UpdateSink`.
The run timeline is the durable audit record.

## 8. Agent (`coding.AgentClass()` + instructions)

Single agent (multi-agent split deferred). `coding.AgentClass()` returns a
`multiagent.AgentClass{Name:"code-editor", Description, Instructions, Model, Tools:[
list_files, read_file, search, edit_hashline, gofmt, go_test, git_diff]}`. System
prompt (the editing protocol):

```text
You are a careful coding agent in a sandboxed workspace.
1. Before editing, call coding.read_file or coding.search; use the returned ¶PATH#TAG
   header and N:TEXT line numbers.
2. Edit existing files only via coding.edit_hashline; every section starts with ¶PATH#TAG.
3. After a successful edit, the old tag and old line numbers are dead — use the new
   header from the edit result or re-read before the next edit.
4. If an edit is rejected (stale tag, mismatch, parse error), STOP and re-read. Never
   stack edits on stale context.
5. Keep ranges tight; replace only lines whose content changes.
6. Body rows are final content only (+TEXT); never -old or bare context lines.
7. Do not use hashline for formatting — use coding.gofmt.
8. Prefer small patches; review coding.git_diff; run focused go_test; report results.
9. Never access paths outside the workspace; never request arbitrary shell; never
   commit/push/delete unless explicitly asked.
```

`packs/coding/coding.go` declares the equivalent as an `api.AgentDefinition` plus
`api.CapabilityManifest`s (one per coding tool) and a few `eval.EvalCase`s. Verify the
exact `api.AgentDefinition`/`api.CapabilityManifest` field names against
`api/agent_definition.go`.

## 9. Testing

- **hashline parser**: parse single/multi section; replace/delete/insert variants;
  reject missing header/tag, invalid tag, `-old` rows, bare context, delete-with-body,
  invalid range.
- **applier**: replace one↔many; delete one/range; insert head/tail/before-first/
  after-last; multiple ops on original line numbers; overlap/out-of-bounds/noop
  rejected; line endings + BOM preserved.
- **snapshot/hash**: same content ⇒ same tag; changed content ⇒ new tag (no store
  needed for these).
- **workspace + tool integration**: read/search return `¶PATH#TAG`; edit with current
  tag succeeds; old tag fails; new header succeeds; multi-file all-succeed → all
  written, one-fail → none written; path-escape/absolute/symlink-escape/`.git`
  rejected.
- **governance**: read allowed under default deny; edit denied without action/policy;
  edit allowed with action+policy; denied edit does not mutate; audit event recorded.
- **agent loop** (scripted provider): read→edit→git_diff→final; read→stale→re-read→
  retry; write_file on existing → redirected to edit_hashline.
- **eval** (M7): use `eval.EvalCase` + `assertions.*` (`PolicyDecisionDeniedBy`,
  `ToolCalledWithArg`, `RunTerminatedWithStatus`, `EventEmitted`) + `eval.RunSuite`.
  NO gate/judge/replay/score artifacts and NO `hydaelyn eval` CLI (explicitly out of
  scope — see the eval-harness decision).

## 10. Milestones & PRs

| Milestone | Content | PR |
|---|---|---|
| M0 | this doc; approve new `coding/` package; lock tag=FNV-low16, store→M6 | — |
| M1 | normalize/hash/format + workspace path resolver + read_file + search (no store) | PR1 |
| M2 | parser + applier + patcher (preflight/commit/all-or-nothing/stale-reject) + edit_hashline (dry_run) | PR2 (dry-run), PR3 (commit) |
| M3 | tool metadata + coding.PolicyEngine + GovernedToolBus integration tests + event audit | PR3 |
| M4 | gofmt (go/format) + go_test + git_diff + command sandbox | PR4 |
| M5 | AgentClass + _examples/coding_hashline + packs/coding manifest | PR5 |
| M6 (done) | MemorySnapshotStore history (bounded per-path LRU) + 3-way recovery (line-level, run-grouped; conservative on duplicate-line bases — see §11) + go/ast block edit (`replace block N` / `delete block N`) | — |
| M7 (done) | eval regression via eval.RunSuite + assertions (stale-conflict / path-escape / policy-deny / all-or-nothing), custom `eval.Harness` wiring `coding.NewToolSet` + `coding.PolicyEngine` through `worker.GovernedToolBus` | — |

Gates: `make verify` per PR; `make ci-local` (incl. `make architecture-check` /
`sentrux check .`) after PR3 and PR5.

## 11. Risk register

| Risk | Mitigation |
|---|---|
| 4-hex tag collision | tag is only the model-facing handle; the fast path (and recovery base) resolve via `UniqueByHash`, which yields a version only when the tag pins to a single recorded content equal to the live file. An out-of-band version that shares the tag but was never recorded, or a tag that maps to two distinct recorded versions (making the edit's line numbers ambiguous), is rejected as stale rather than applied against the wrong version |
| stale line numbers | stale-reject + prompt rule + tests |
| multi-file partial write | preflight all + rollback buffer (§4.7) |
| duplicate sections aliasing one file | the duplicate-section guard keys on the resolved on-disk identity (`identityResolver.ResolveIdentity`), so two in-root symlink aliases for the same file (e.g. `a.go` and `link.go → a.go`) are rejected up front instead of the second `Commit` write clobbering the first's edit |
| 3-way merge silent data loss | LCS alignment is only sound on distinct-line bases; trivial cases (one side unchanged / both identical) short-circuit, and an ambiguous duplicate-line base conflicts and falls back to stale-reject rather than mis-merge |
| path escape | resolver + parent-dir symlink check |
| symlink alias into denied tree | `.git` deny enforced on the canonical resolved target, not just the lexical path, so an alias like `g -> .git` cannot reach `.git/**` |
| arbitrary command exec | no shell, strict allowlist, timeout, output cap, env scrub (GOFLAGS excluded from passthrough; GOENV=off so the per-user `go env` file cannot reintroduce it) |
| repo config → command exec on a read-only diff | `git_diff` runs `-c core.fsmonitor=false --no-ext-diff --no-textconv`, disabling a `.git/config` filesystem-monitor hook, `diff.external`, and textconv helpers; the allowlist admits only the exact `core.fsmonitor=false` override |
| formatter fights hashline | hashline forbids formatting; separate gofmt tool |
| parser too permissive | strict grammar + typed errors |
| policy bypass | tools only reachable via GovernedToolBus in the worker path |
| large files | max bytes, line ranges, search snippets |

## 12. Definition of Done (v1 MVP)

1 read/search return `¶PATH#TAG` numbered content. 2 existing files modified only via
`coding.edit_hashline`. 3 stale-tag edit rejected, no mutation. 4 multi-section patch
all-or-nothing. 5 edit returns fresh header + compact diff. 6 write tools policy-gated
+ audited via events. 7 gofmt/go_test/git_diff available and sandboxed. 8 path-escape +
non-allowlisted commands tested. 9 `_examples/coding_hashline` demonstrates
read→edit→gofmt→test→diff with no shell access. 10 governance + integration tests pass;
`make verify` green.
