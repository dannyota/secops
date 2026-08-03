package cli

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunHedgedDoctorProbeDelaysFallback(t *testing.T) {
	primaryStarted := make(chan struct{})
	primaryCanceled := make(chan struct{})
	fallbackStarted := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- runHedgedDoctorProbe(
			context.Background(),
			25*time.Millisecond,
			func(ctx context.Context) error {
				close(primaryStarted)
				<-ctx.Done()
				close(primaryCanceled)
				return ctx.Err()
			},
			func(context.Context) error {
				close(fallbackStarted)
				return nil
			},
		)
	}()

	awaitSignal(t, primaryStarted, "primary start")
	select {
	case <-fallbackStarted:
		t.Fatal("fallback started before the hedge delay")
	default:
	}
	awaitSignal(t, fallbackStarted, "delayed fallback start")
	if err := awaitResult(t, done); err != nil {
		t.Fatalf("runHedgedDoctorProbe() error = %v, want nil", err)
	}
	awaitSignal(t, primaryCanceled, "primary cancellation")
}

func TestRunHedgedDoctorProbeStartsFallbackAfterEarlyError(t *testing.T) {
	primaryErr := errors.New("primary unavailable")
	fallbackStarted := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- runHedgedDoctorProbe(
			context.Background(),
			time.Hour,
			func(context.Context) error { return primaryErr },
			func(context.Context) error {
				close(fallbackStarted)
				return nil
			},
		)
	}()

	awaitSignal(t, fallbackStarted, "fallback start after primary error")
	if err := awaitResult(t, done); err != nil {
		t.Fatalf("runHedgedDoctorProbe() error = %v, want nil", err)
	}
}

func TestRunHedgedDoctorProbeFirstSuccessCancelsPeer(t *testing.T) {
	primaryStarted := make(chan struct{})
	fallbackStarted := make(chan struct{})
	fallbackCanceled := make(chan struct{})
	releasePrimary := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- runHedgedDoctorProbe(
			context.Background(),
			0,
			func(context.Context) error {
				close(primaryStarted)
				<-releasePrimary
				return nil
			},
			func(ctx context.Context) error {
				close(fallbackStarted)
				<-ctx.Done()
				close(fallbackCanceled)
				return ctx.Err()
			},
		)
	}()

	awaitSignal(t, primaryStarted, "primary start")
	awaitSignal(t, fallbackStarted, "fallback start")
	close(releasePrimary)
	if err := awaitResult(t, done); err != nil {
		t.Fatalf("runHedgedDoctorProbe() error = %v, want nil", err)
	}
	awaitSignal(t, fallbackCanceled, "fallback cancellation")
}

func TestRunHedgedDoctorProbeWaitsForSuccessAfterError(t *testing.T) {
	primaryErr := errors.New("primary failed")
	releaseFallback := make(chan struct{})
	fallbackStarted := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		done <- runHedgedDoctorProbe(
			context.Background(),
			time.Hour,
			func(context.Context) error { return primaryErr },
			func(context.Context) error {
				close(fallbackStarted)
				<-releaseFallback
				return nil
			},
		)
	}()

	awaitSignal(t, fallbackStarted, "fallback start")
	select {
	case err := <-done:
		t.Fatalf("returned after first error with %v; want to wait for fallback", err)
	default:
	}
	close(releaseFallback)
	if err := awaitResult(t, done); err != nil {
		t.Fatalf("runHedgedDoctorProbe() error = %v, want nil", err)
	}
}

func TestRunHedgedDoctorProbeJoinsBothErrors(t *testing.T) {
	primaryErr := errors.New("HTTP/2 failed")
	fallbackErr := errors.New("HTTP/1.1 failed")

	err := runHedgedDoctorProbe(
		context.Background(),
		0,
		func(context.Context) error { return primaryErr },
		func(context.Context) error { return fallbackErr },
	)
	if !errors.Is(err, primaryErr) {
		t.Errorf("error %v does not wrap primary error", err)
	}
	if !errors.Is(err, fallbackErr) {
		t.Errorf("error %v does not wrap fallback error", err)
	}
	if !strings.Contains(err.Error(), "primary probe: HTTP/2 failed") {
		t.Errorf("error %q does not label primary failure", err)
	}
	if !strings.Contains(err.Error(), "fallback probe: HTTP/1.1 failed") {
		t.Errorf("error %q does not label fallback failure", err)
	}
}

func TestRunHedgedDoctorProbeDeadlineCancelsBoth(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()

	primaryStarted := make(chan struct{})
	fallbackStarted := make(chan struct{})
	primaryCanceled := make(chan struct{})
	fallbackCanceled := make(chan struct{})

	err := runHedgedDoctorProbe(
		ctx,
		0,
		func(ctx context.Context) error {
			close(primaryStarted)
			<-ctx.Done()
			close(primaryCanceled)
			return ctx.Err()
		},
		func(ctx context.Context) error {
			close(fallbackStarted)
			<-ctx.Done()
			close(fallbackCanceled)
			return ctx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runHedgedDoctorProbe() error = %v, want deadline exceeded", err)
	}
	awaitSignal(t, primaryStarted, "primary start")
	awaitSignal(t, fallbackStarted, "fallback start")
	awaitSignal(t, primaryCanceled, "primary deadline cancellation")
	awaitSignal(t, fallbackCanceled, "fallback deadline cancellation")
}

func awaitSignal(t *testing.T, ch <-chan struct{}, name string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", name)
	}
}

func awaitResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for probe result")
		return nil
	}
}
