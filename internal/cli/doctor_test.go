package cli

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"danny.vn/secops/config"
)

func TestEffectiveDoctorTimeout(t *testing.T) {
	saved := requestTimeout
	defer func() { requestTimeout = saved }()

	cmd := &cobra.Command{Use: "doctor-test"}
	cmd.Flags().DurationVar(&requestTimeout, "timeout", defaultRequestTimeout, "test timeout")
	if got := effectiveDoctorTimeout(cmd); got != defaultDoctorTimeout {
		t.Fatalf("default effectiveDoctorTimeout = %v, want %v", got, defaultDoctorTimeout)
	}
	if err := cmd.Flags().Set("timeout", "2s"); err != nil {
		t.Fatalf("set timeout: %v", err)
	}
	if got := effectiveDoctorTimeout(cmd); got != 2*time.Second {
		t.Fatalf("explicit effectiveDoctorTimeout = %v, want 2s", got)
	}
	if err := cmd.Flags().Set("timeout", "0"); err != nil {
		t.Fatalf("disable timeout: %v", err)
	}
	if got := effectiveDoctorTimeout(cmd); got != 0 {
		t.Fatalf("disabled effectiveDoctorTimeout = %v, want 0", got)
	}
}

func TestProbeSOARProtocolsHealthyPrimaryDoesNotStartFallback(t *testing.T) {
	var h1Requests atomic.Int32
	var h2Requests atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSOARVersionProbeRequest(t, r)
		switch r.ProtoMajor {
		case 2:
			h2Requests.Add(1)
		case 1:
			h1Requests.Add(1)
		}
		_, _ = io.WriteString(w, `{"payload":"test-version"}`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	inst := testDoctorSOARInstance(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := probeSOARProtocols(ctx, inst, time.Second, 50*time.Millisecond, doctorTestTransportFactory(t, server)); err != nil {
		t.Fatalf("probeSOARProtocols: %v", err)
	}
	// The helper stops its timer on primary success; waiting past the hedge delay
	// proves no detached fallback request appears later.
	time.Sleep(75 * time.Millisecond)
	if got := h2Requests.Load(); got != 1 {
		t.Fatalf("HTTP/2 requests = %d, want one successful primary", got)
	}
	if got := h1Requests.Load(); got != 0 {
		t.Fatalf("HTTP/1.1 requests = %d, want no fallback", got)
	}
}

func TestProbeSOARProtocolsHTTP1WinsAndCancelsStalledHTTP2Body(t *testing.T) {
	var h1Requests atomic.Int32
	var h2Requests atomic.Int32
	h2Canceled := make(chan struct{})
	releaseH2 := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseH2) }) }

	requestErrors := make(chan error, 4)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := validateSOARVersionProbeRequest(r); err != nil {
			requestErrors <- err
		}
		switch r.ProtoMajor {
		case 2:
			h2Requests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				close(h2Canceled)
			case <-releaseH2:
			}
		case 1:
			h1Requests.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"payload":"test-version"}`)
		default:
			requestErrors <- fmt.Errorf("unexpected HTTP protocol %s", r.Proto)
		}
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer func() {
		release()
		server.Close()
	}()

	inst := testDoctorSOARInstance(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := probeSOARProtocols(ctx, inst, 2*time.Second, 25*time.Millisecond, doctorTestTransportFactory(t, server)); err != nil {
		t.Fatalf("probeSOARProtocols: %v", err)
	}
	select {
	case <-h2Canceled:
	case <-time.After(time.Second):
		release()
		t.Fatal("stalled HTTP/2 response body was not canceled after HTTP/1.1 succeeded")
	}
	if got := h2Requests.Load(); got != 1 {
		t.Errorf("HTTP/2 requests = %d, want 1", got)
	}
	if got := h1Requests.Load(); got != 1 {
		t.Errorf("HTTP/1.1 requests = %d, want 1", got)
	}
	select {
	case err := <-requestErrors:
		t.Error(err)
	default:
	}
}

func TestProbeSOARProtocolsShortDeadlineStillStartsFallback(t *testing.T) {
	var requests atomic.Int32
	primaryCanceled := make(chan struct{})
	releasePrimary := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releasePrimary) }) }

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertSOARVersionProbeRequest(t, r)
		if requests.Add(1) == 1 {
			w.WriteHeader(http.StatusOK)
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			select {
			case <-r.Context().Done():
				close(primaryCanceled)
			case <-releasePrimary:
			}
			return
		}
		_, _ = io.WriteString(w, `{"payload":"test-version"}`)
	}))
	defer func() {
		release()
		server.Close()
	}()

	inst := testDoctorSOARInstance(server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := probeSOARProtocols(ctx, inst, 200*time.Millisecond, doctorSOARHedgeDelay, func(bool) *http.Transport {
		return http.DefaultTransport.(*http.Transport).Clone()
	}); err != nil {
		t.Fatalf("probeSOARProtocols with short deadline: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("version endpoint received %d requests, want stalled primary plus fallback", got)
	}
	select {
	case <-primaryCanceled:
	case <-time.After(time.Second):
		release()
		t.Fatal("short-deadline fallback success did not cancel the primary")
	}
}

func doctorTestTransportFactory(t *testing.T, server *httptest.Server) doctorTransportFactory {
	t.Helper()
	serverTransport, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatalf("httptest client transport is %T, want *http.Transport", server.Client().Transport)
	}
	return func(bool) *http.Transport {
		transport := serverTransport.Clone()
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
		transport.TLSClientConfig.MinVersion = tls.VersionTLS12
		protocols := new(http.Protocols)
		protocols.SetHTTP1(true)
		protocols.SetHTTP2(true)
		transport.Protocols = protocols
		transport.ForceAttemptHTTP2 = true
		return transport
	}
}

func testDoctorSOARInstance(baseURL string) *config.Instance {
	return &config.Instance{
		ProjectNumber: "123",
		Region:        "us",
		CustomerID:    "customer",
		SOARURL:       baseURL,
		SOARAppKey:    "test-app-key",
	}
}

func assertSOARVersionProbeRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if err := validateSOARVersionProbeRequest(r); err != nil {
		t.Error(err)
	}
}

func validateSOARVersionProbeRequest(r *http.Request) error {
	if r.Method != http.MethodGet {
		return fmt.Errorf("method = %s, want GET", r.Method)
	}
	wantPath := "/v1alpha/projects/123/locations/us/instances/customer/legacySystem:legacyGetSystemVersion"
	if r.URL.Path != wantPath {
		return fmt.Errorf("path = %q, want %q", r.URL.Path, wantPath)
	}
	if got := r.URL.Query().Get("format"); got != "camel" {
		return fmt.Errorf("format = %q, want camel", got)
	}
	if got := r.Header.Get("AppKey"); got != "test-app-key" {
		return fmt.Errorf("AppKey = %q, want test-app-key", got)
	}
	return nil
}
