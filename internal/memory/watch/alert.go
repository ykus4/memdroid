package watch

import (
	"fmt"
	"sync"
	"time"

	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

// AlertCondition defines when an alert fires.
type AlertCondition int

const (
	AlertAbove   AlertCondition = iota // value > threshold
	AlertBelow                         // value < threshold
	AlertChanged                       // any change
)

// AlertAction defines what to do when the condition is met.
type AlertAction int

const (
	ActionNotify AlertAction = iota // just notify via OnAlert
	ActionWrite                     // write a value when triggered
)

// AlertConfig configures a conditional watch.
type AlertConfig struct {
	Addr      uintptr
	Condition AlertCondition
	Threshold []byte // for Above/Below comparisons
	Action    AlertAction
	WriteVal  []byte // value to write when ActionWrite
}

// AlertEvent is emitted when an alert fires.
type AlertEvent struct {
	Addr      uintptr
	Condition string
	Value     string
	Triggered bool // whether an action was taken
}

type alertEntry struct {
	cfg  AlertConfig
	drv  driver.Driver
	pid  int
	vt   search.ValueType
	stop chan struct{}
}

// AlertWatcher manages conditional watches with automatic actions.
type AlertWatcher struct {
	mu      sync.Mutex
	entries map[uintptr]*alertEntry
	OnAlert func(AlertEvent)
}

func NewAlertWatcher() *AlertWatcher {
	return &AlertWatcher{entries: make(map[uintptr]*alertEntry)}
}

func ParseAlertCondition(s string) (AlertCondition, error) {
	switch s {
	case "above", ">":
		return AlertAbove, nil
	case "below", "<":
		return AlertBelow, nil
	case "changed", "!=":
		return AlertChanged, nil
	default:
		return 0, fmt.Errorf("unknown condition: %s (use: above, below, changed)", s)
	}
}

func (c AlertCondition) String() string {
	switch c {
	case AlertAbove:
		return "above"
	case AlertBelow:
		return "below"
	case AlertChanged:
		return "changed"
	default:
		return "?"
	}
}

// WatchWithAlert starts a conditional watch. When the condition fires, the
// action is executed (notify or write).
func (aw *AlertWatcher) WatchWithAlert(drv driver.Driver, pid int, vt search.ValueType, cfg AlertConfig, interval time.Duration) error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	if _, exists := aw.entries[cfg.Addr]; exists {
		return fmt.Errorf("alert already set for 0x%x", cfg.Addr)
	}

	e := &alertEntry{cfg: cfg, drv: drv, pid: pid, vt: vt, stop: make(chan struct{})}
	aw.entries[cfg.Addr] = e

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var lastVal []byte
		for {
			select {
			case <-ticker.C:
				cur, err := e.drv.Peek(e.pid, e.cfg.Addr, e.vt.Size())
				if err != nil {
					continue
				}

				fired := false
				switch e.cfg.Condition {
				case AlertAbove:
					fired = search.CompareValues(cur, e.cfg.Threshold, e.vt) > 0
				case AlertBelow:
					fired = search.CompareValues(cur, e.cfg.Threshold, e.vt) < 0
				case AlertChanged:
					if lastVal != nil {
						fired = !search.EqualBytes(cur, lastVal)
					}
				}

				if fired {
					triggered := false
					if e.cfg.Action == ActionWrite && len(e.cfg.WriteVal) > 0 {
						_ = e.drv.Poke(e.pid, e.cfg.Addr, e.cfg.WriteVal)
						triggered = true
					}
					if aw.OnAlert != nil {
						aw.OnAlert(AlertEvent{
							Addr:      e.cfg.Addr,
							Condition: e.cfg.Condition.String(),
							Value:     search.FormatValue(cur, e.vt),
							Triggered: triggered,
						})
					}
				}

				if lastVal == nil {
					lastVal = make([]byte, len(cur))
				}
				copy(lastVal, cur)
			case <-e.stop:
				return
			}
		}
	}()

	return nil
}

// RemoveAlert stops a conditional watch.
func (aw *AlertWatcher) RemoveAlert(addr uintptr) error {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	e, ok := aw.entries[addr]
	if !ok {
		return fmt.Errorf("no alert set for 0x%x", addr)
	}
	close(e.stop)
	delete(aw.entries, addr)
	return nil
}

// RemoveAll stops all alerts.
func (aw *AlertWatcher) RemoveAll() {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	for addr, e := range aw.entries {
		close(e.stop)
		delete(aw.entries, addr)
	}
}

// List returns all alert addresses.
func (aw *AlertWatcher) List() []uintptr {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	addrs := make([]uintptr, 0, len(aw.entries))
	for addr := range aw.entries {
		addrs = append(addrs, addr)
	}
	return addrs
}
