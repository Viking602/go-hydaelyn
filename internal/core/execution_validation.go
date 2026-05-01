package core

func validateTaskHolder(task Task, holderType HolderType, holderID string) error {
	switch holderType {
	case HolderAgent:
		if task.OwnerAgentID != holderID {
			return ErrLeaseHolderMismatch
		}
	case HolderComponent:
		if task.OwnerComponent != holderID {
			return ErrLeaseHolderMismatch
		}
	default:
		return ErrInvalidCommand
	}
	return nil
}
