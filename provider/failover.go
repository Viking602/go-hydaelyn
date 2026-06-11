package provider

import (
	"context"
	"errors"
	"fmt"
)

// Fallback returns a Driver that tries primary's Stream first and, when
// initiation fails, tries each fallback in order — model failover for
// provider outages that survive the driver's own retry policy. It never
// fails over mid-stream: once any driver returns a Stream, that stream
// is the run's. Metadata reports the primary's identity so resolver
// registration and usage attribution stay stable.
func Fallback(primary Driver, fallbacks ...Driver) Driver {
	drivers := make([]Driver, 0, 1+len(fallbacks))
	drivers = append(drivers, primary)
	drivers = append(drivers, fallbacks...)
	return fallbackDriver{drivers: drivers}
}

type fallbackDriver struct {
	drivers []Driver
}

func (d fallbackDriver) Metadata() Metadata {
	for _, driver := range d.drivers {
		if driver != nil {
			return driver.Metadata()
		}
	}
	return Metadata{}
}

func (d fallbackDriver) Stream(ctx context.Context, request Request) (Stream, error) {
	var errs []error
	for _, driver := range d.drivers {
		if driver == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			errs = append(errs, err)
			break
		}
		stream, err := driver.Stream(ctx, request)
		if err == nil {
			return stream, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", driver.Metadata().Name, err))
	}
	if len(errs) == 0 {
		return nil, errors.New("provider: fallback chain holds no drivers")
	}
	return nil, fmt.Errorf("provider: every fallback driver failed: %w", errors.Join(errs...))
}
