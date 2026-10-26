//go:build linux && !baremetal

package bluetooth

import (
	"sync"
	"testing"
)

func TestScanLifecycleStopDuringSetup(t *testing.T) {
	var lifecycle scanLifecycle

	session, err := lifecycle.start()
	if err != nil {
		t.Fatalf("start scan: %v", err)
	}

	setupStarted := make(chan struct{})
	finishSetup := make(chan struct{})
	scanReturned := make(chan bool, 1)
	go func() {
		close(setupStarted)
		<-finishSetup
		scanReturned <- lifecycle.cancelled(session)
		lifecycle.finish(session)
	}()
	<-setupStarted

	if err := lifecycle.stop(); err != nil {
		t.Fatalf("stop scan during setup: %v", err)
	}
	if _, err := lifecycle.start(); err != errScanning {
		t.Fatalf("start overlapping scan error = %v, want %v", err, errScanning)
	}

	close(finishSetup)
	if !<-scanReturned {
		t.Fatal("scan setup did not observe cancellation")
	}
	if err := lifecycle.stop(); err != errNotScanning {
		t.Fatalf("stop after scan returned error = %v, want %v", err, errNotScanning)
	}

	if _, err := lifecycle.start(); err != nil {
		t.Fatalf("start after scan returned: %v", err)
	}
}

func TestScanLifecycleConcurrentStopClosesOnce(t *testing.T) {
	var lifecycle scanLifecycle
	if _, err := lifecycle.start(); err != nil {
		t.Fatalf("start scan: %v", err)
	}

	const callers = 8
	var wait sync.WaitGroup
	errors := make(chan error, callers)
	for range callers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errors <- lifecycle.stop()
		}()
	}
	wait.Wait()
	close(errors)

	stopped := 0
	for err := range errors {
		if err == nil {
			stopped++
		} else if err != errNotScanning {
			t.Errorf("stop scan error = %v, want nil or %v", err, errNotScanning)
		}
	}
	if stopped != 1 {
		t.Fatalf("successful stops = %d, want 1", stopped)
	}
}
