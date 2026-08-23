package contract

import "testing"

// RunRunStoreContractTests exercises run / task / event durability.
// Subtest names match the locked full-suite names (ADR-022).
func RunRunStoreContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	requireFactory(t, factory)
	t.Run("CRUD", func(t *testing.T) { runCRUDSuite(t, factory) })
	t.Run("Transactions", func(t *testing.T) { runTransactionSuite(t, factory) })
	t.Run("EventOrdering", func(t *testing.T) { runEventOrderingSuite(t, factory) })
	t.Run("ReplayDeterminism", func(t *testing.T) { runReplayDeterminismSuite(t, factory) })
}

// RunObservabilityStoreContractTests exercises lease CAS.
func RunObservabilityStoreContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	requireFactory(t, factory)
	t.Run("LeaseCAS", func(t *testing.T) { runLeaseCASSuite(t, factory) })
}

// RunMessagingStoreContractTests exercises resume tokens and outbox FIFO.
func RunMessagingStoreContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	requireFactory(t, factory)
	t.Run("ResumeAndOutbox", func(t *testing.T) { runResumeAndOutboxSuite(t, factory) })
}

// RunCollaborationStoreContractTests exercises multi-agent stores.
func RunCollaborationStoreContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	requireFactory(t, factory)
	t.Run("MultiAgentStores", func(t *testing.T) { runMultiAgentStoreSuite(t, factory) })
}

// RunIdentityStoreContractTests exercises AgentDefinition snapshots.
func RunIdentityStoreContractTests(t *testing.T, factory ProviderFactory) {
	t.Helper()
	requireFactory(t, factory)
	t.Run("AgentDefinitionSnapshots", func(t *testing.T) { runAgentDefinitionSnapshotSuite(t, factory) })
}

func requireFactory(t *testing.T, factory ProviderFactory) {
	t.Helper()
	if factory == nil {
		t.Fatal("contract: ProviderFactory must not be nil")
	}
}
