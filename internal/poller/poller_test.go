package poller

import "testing"

func TestPoolLifecycle(t *testing.T) {
	p := New()

	started := make(chan struct{})
	done := make(chan struct{})

	err := p.Start(0x10, func(stop <-chan struct{}) {
		close(started)
		<-stop
		close(done)
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	<-started

	if !p.Has(0x10) {
		t.Errorf("Has(0x10) should be true")
	}
	if got := p.List(); len(got) != 1 || got[0] != 0x10 {
		t.Errorf("List = %v, want [0x10]", got)
	}

	// Starting the same key again must fail.
	if err := p.Start(0x10, func(<-chan struct{}) {}); err == nil {
		t.Errorf("duplicate Start should error")
	}

	if err := p.Stop(0x10); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	<-done // task goroutine observed the stop signal
	if p.Has(0x10) {
		t.Errorf("Has(0x10) should be false after Stop")
	}
	if err := p.Stop(0x10); err == nil {
		t.Errorf("Stop of missing key should error")
	}
}

func TestPoolListSortedAndStopAll(t *testing.T) {
	p := New()
	for _, k := range []uintptr{0x30, 0x10, 0x20} {
		if err := p.Start(k, func(stop <-chan struct{}) { <-stop }); err != nil {
			t.Fatalf("Start(0x%x): %v", k, err)
		}
	}
	got := p.List()
	want := []uintptr{0x10, 0x20, 0x30}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("List = %v, want %v", got, want)
		}
	}
	p.StopAll()
	if len(p.List()) != 0 {
		t.Errorf("StopAll should clear all tasks")
	}
}
