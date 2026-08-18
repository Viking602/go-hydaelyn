package model

import "strings"

const hydaelynSelfNamespace = "hydaelyn.self."

func ValidateCapabilityName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ErrInvalidCommand
	}
	if strings.HasPrefix(trimmed, hydaelynSelfNamespace) {
		return ErrCapabilityNameReserved
	}
	return nil
}
