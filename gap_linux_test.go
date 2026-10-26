//go:build linux && !baremetal

package bluetooth

import (
	"sync"
	"testing"

	"github.com/godbus/dbus/v5"
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

func TestResetRejectsScanUntilScanReturns(t *testing.T) {
	tests := []struct {
		name    string
		stopped bool
	}{
		{name: "running"},
		{name: "stopped but cleaning up", stopped: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bus := &dbus.Conn{}
			bluez := &dbus.Object{}
			adapter := &dbus.Object{}
			a := &Adapter{
				bus:     bus,
				bluez:   bluez,
				adapter: adapter,
				address: "01:23:45:67:89:AB",
			}

			cancelChan, err := a.scan.start()
			if err != nil {
				t.Fatalf("start scan: %v", err)
			}
			defer a.scan.finish(cancelChan)
			if test.stopped {
				if err := a.scan.stop(); err != nil {
					t.Fatalf("stop scan: %v", err)
				}
			}

			if err := a.Reset(); err != errScanning {
				t.Fatalf("reset error = %v, want %v", err, errScanning)
			}
			if a.bus != bus || a.bluez != bluez || a.adapter != adapter {
				t.Fatal("reset cleared D-Bus state while scan was active")
			}
			if a.address != "01:23:45:67:89:AB" {
				t.Fatalf("reset cleared adapter address during scan: %q", a.address)
			}
			if got := a.scan.cancelled(cancelChan); got != test.stopped {
				t.Fatalf("scan cancelled = %t, want %t", got, test.stopped)
			}
		})
	}
}
