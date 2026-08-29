package testbackend_test

import (
	"testing"

	"github.com/Viking602/venat/durable"
	"github.com/Viking602/venat/durable/contract"
	"github.com/Viking602/venat/durable/internal/testbackend"
)

func TestBackendContract(t *testing.T) {
	contract.RunBackendContractTests(t, func(*testing.T) (durable.Backend, func(*testing.T) durable.Backend, func()) {
		backend := testbackend.New()
		return backend, func(*testing.T) durable.Backend { return backend.Reopen() }, func() {}
	})
}
