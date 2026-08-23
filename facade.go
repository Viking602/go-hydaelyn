package venat

// RunAdmin is the registration, pipeline, and raw-store administration
// surface. Application code should keep using typed lifecycle methods on
// Runner; pass Admin() when a helper must touch stores or registries.
//
// Spec anchor: ADR-025.
type RunAdmin struct{ *Runner }

// Governance is the lease, approval, action-attempt, usage, and trace
// surface. Spec anchor: ADR-025.
type Governance struct{ *Runner }

// Blackboard is the run-scoped working-memory surface.
// Spec anchor: ADR-025.
type Blackboard struct{ *Runner }

// Admin returns the administration sub-façade.
func (r *Runner) Admin() RunAdmin { return RunAdmin{Runner: r} }

// Governance returns the governance sub-façade.
func (r *Runner) Governance() Governance { return Governance{Runner: r} }

// Blackboard returns the blackboard sub-façade.
func (r *Runner) Blackboard() Blackboard { return Blackboard{Runner: r} }
