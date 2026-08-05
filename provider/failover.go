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
// is the run's. Stream identity reports the selected driver's identity,
// rather than the wrapper's primary metadata.
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
			return identifyStream(stream, StreamIdentity{
				Provider: driver.Metadata(),
				Model:    request.Model,
			}), nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", driver.Metadata().Name, err))
	}
	if len(errs) == 0 {
		return nil, errors.New("provider: fallback chain holds no drivers")
	}
	return nil, fmt.Errorf("provider: every fallback driver failed: %w", errors.Join(errs...))
}

// ModelFallback returns a Driver that switches both driver and request model
// when the primary cannot open a stream. It never switches after a stream has
// been established.
func ModelFallback(primary Driver, fallback Driver, fallbackModel string) Driver {
	return modelFallbackDriver{
		primary:       primary,
		fallback:      fallback,
		fallbackModel: fallbackModel,
	}
}

type modelFallbackDriver struct {
	primary       Driver
	fallback      Driver
	fallbackModel string
}

func (d modelFallbackDriver) Metadata() Metadata {
	if d.primary == nil {
		return Metadata{}
	}
	return d.primary.Metadata()
}

func (d modelFallbackDriver) Stream(ctx context.Context, request Request) (Stream, error) {
	if d.primary == nil || d.fallback == nil || d.fallbackModel == "" {
		return nil, errors.New("provider: model fallback is incomplete")
	}
	primaryModel := request.Model
	stream, primaryErr := d.primary.Stream(ctx, request)
	if primaryErr == nil {
		return identifyStream(stream, StreamIdentity{
			Provider: d.primary.Metadata(),
			Model:    primaryModel,
		}), nil
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(primaryErr, err)
	}
	request.Model = d.fallbackModel
	stream, fallbackErr := d.fallback.Stream(ctx, request)
	if fallbackErr == nil {
		return identifyStream(stream, StreamIdentity{
			Provider: d.fallback.Metadata(),
			Model:    d.fallbackModel,
		}), nil
	}
	return nil, fmt.Errorf(
		"provider: primary model %q and fallback model %q failed: %w",
		primaryModel,
		d.fallbackModel,
		errors.Join(primaryErr, fallbackErr),
	)
}
