package scheduler_test

import (
	"testing"

	"github.com/Viking602/venat/transport/scheduler"
)

func TestDeprecatedSchedulerPackageAliasesCronDriver(t *testing.T) {
	driver := scheduler.New(scheduler.Options{})
	if driver == nil {
		t.Fatal("scheduler.New returned nil")
	}
	if got := driver.List(); len(got) != 0 {
		t.Fatalf("new driver should start with no registrations, got %d", len(got))
	}
}
