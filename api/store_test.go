package api

import "testing"

func TestUnitOfWorkEmbedsCapabilityInterfaces(t *testing.T) {
	var uow UnitOfWork
	var _ RunStores = uow
	var _ CollaborationStores = uow
	var _ MessagingStores = uow
	var _ GovernanceStores = uow
	var _ IdentityStores = uow
	var _ ObservabilityStores = uow
}
