package watch

import (
	"fmt"
	"time"

	"memdroid/internal/driver"
	"memdroid/internal/memory/search"
	"memdroid/internal/poller"
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

// AlertWatcher manages conditional watches with automatic actions.
// OnAlert is invoked from watcher goroutines and must be safe for concurrent use.
type AlertWatcher struct {
	pool    *poller.Pool
	OnAlert func(AlertEvent)
}

func NewAlertWatcher() *AlertWatcher {
	return &AlertWatcher{pool: poller.New()}
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
	size := vt.Size()
	if size == 0 {
		return fmt.Errorf("alert does not support the %s type", vt)
	}
	if cfg.Condition != AlertChanged && len(cfg.Threshold) < size {
		return fmt.Errorf("threshold too short for %s (need %d bytes)", vt, size)
	}

	return aw.pool.Start(cfg.Addr, func(stop <-chan struct{}) {
		var lastVal []byte
		poller.EveryTick(interval, stop, func() {
			cur, err := drv.Peek(pid, cfg.Addr, size)
			if err != nil {
				return
			}

			fired := false
			switch cfg.Condition {
			case AlertAbove:
				fired = search.CompareValues(cur, cfg.Threshold, vt) > 0
			case AlertBelow:
				fired = search.CompareValues(cur, cfg.Threshold, vt) < 0
			case AlertChanged:
				fired = lastVal != nil && !search.EqualBytes(cur, lastVal)
			}

			if fired {
				triggered := false
				if cfg.Action == ActionWrite && len(cfg.WriteVal) > 0 {
					_ = drv.Poke(pid, cfg.Addr, cfg.WriteVal)
					triggered = true
				}
				if aw.OnAlert != nil {
					aw.OnAlert(AlertEvent{
						Addr:      cfg.Addr,
						Condition: cfg.Condition.String(),
						Value:     search.FormatValue(cur, vt),
						Triggered: triggered,
					})
				}
			}

			if lastVal == nil {
				lastVal = make([]byte, len(cur))
			}
			copy(lastVal, cur)
		})
	})
}

// RemoveAlert stops a conditional watch.
func (aw *AlertWatcher) RemoveAlert(addr uintptr) error { return aw.pool.Stop(addr) }

// RemoveAll stops all alerts.
func (aw *AlertWatcher) RemoveAll() { aw.pool.StopAll() }

// List returns all alert addresses.
func (aw *AlertWatcher) List() []uintptr { return aw.pool.List() }
