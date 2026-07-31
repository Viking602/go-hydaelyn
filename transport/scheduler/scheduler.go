// Package scheduler is a deprecated compatibility shim for the cron trigger
// transport.
//
// Deprecated: use github.com/Viking602/venat/transport/cron.
package scheduler

import "github.com/Viking602/venat/transport/cron"

// Driver installs api.TriggerSchedule firings on top of robfig/cron.
//
// Deprecated: use cron.Driver.
type Driver = cron.Driver

// Options configures Driver construction.
//
// Deprecated: use cron.Options.
type Options = cron.Options

// New constructs a cron trigger transport driver.
//
// Deprecated: use cron.New.
func New(opts Options) *Driver {
	return cron.New(opts)
}
