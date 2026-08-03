package cli

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// doctorProbe is a small, read-only health check. A probe must honor ctx so a
// slower duplicate can be stopped after another probe succeeds.
type doctorProbe func(context.Context) error

type doctorProbeResult struct {
	primary bool
	err     error
}

// runHedgedDoctorProbe runs primary immediately and starts fallback after
// hedgeDelay. If primary fails before that delay, fallback starts immediately.
// The first successful probe wins and cancels the other; an error only wins
// after both probes have failed.
func runHedgedDoctorProbe(
	ctx context.Context,
	hedgeDelay time.Duration,
	primary doctorProbe,
	fallback doctorProbe,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	probeCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Each probe sends at most once. Capacity for both results lets a probe
	// finish after the caller has returned without blocking its goroutine.
	results := make(chan doctorProbeResult, 2)
	launch := func(isPrimary bool, probe doctorProbe) {
		go func() {
			results <- doctorProbeResult{primary: isPrimary, err: probe(probeCtx)}
		}()
	}

	launch(true, primary)

	fallbackStarted := false
	startFallback := func() {
		if fallbackStarted {
			return
		}
		fallbackStarted = true
		launch(false, fallback)
	}

	var timer *time.Timer
	var timerC <-chan time.Time
	if hedgeDelay <= 0 {
		startFallback()
	} else {
		timer = time.NewTimer(hedgeDelay)
		timerC = timer.C
		defer func() {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		}()
	}

	var primaryErr, fallbackErr error
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-timerC:
			timerC = nil
			startFallback()

		case result := <-results:
			if result.err == nil {
				if err := ctx.Err(); err != nil {
					return err
				}
				cancel()
				return nil
			}

			if result.primary {
				primaryErr = result.err
				if !fallbackStarted {
					if timer != nil {
						timer.Stop()
						timerC = nil
					}
					startFallback()
				}
			} else {
				fallbackErr = result.err
			}

			if primaryErr != nil && fallbackErr != nil {
				return errors.Join(
					fmt.Errorf("primary probe: %w", primaryErr),
					fmt.Errorf("fallback probe: %w", fallbackErr),
				)
			}
		}
	}
}
