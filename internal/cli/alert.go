package cli

import (
	"fmt"
	"time"

	"memdroid/internal/app"
	"memdroid/internal/memory/watch"
)

// DefaultAlertPollInterval is the default polling interval for alert watches.
const DefaultAlertPollInterval = "500ms"

func SetAlert(st *app.State) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	fmt.Println("Condition: above, below, changed")
	cond, err := watch.ParseAlertCondition(Prompt("Condition: "))
	if err != nil {
		fmt.Printf("%v\n", err)
		return
	}
	vt := st.GetValueType()
	cfg := watch.AlertConfig{
		Addr:      addr,
		Condition: cond,
		Action:    watch.ActionNotify,
	}
	if cond != watch.AlertChanged {
		threshold, ok := ParseValue("Threshold value: ", vt)
		if !ok {
			return
		}
		cfg.Threshold = threshold
	}
	action := Prompt("Action (notify / write) [default: notify]: ")
	if action == "write" {
		cfg.Action = watch.ActionWrite
		writeVal, ok := ParseValue("Value to write when triggered: ", vt)
		if !ok {
			return
		}
		cfg.WriteVal = writeVal
	}
	intervalStr := Prompt(fmt.Sprintf("Poll interval [default: %s]: ", DefaultAlertPollInterval))
	if intervalStr == "" {
		intervalStr = DefaultAlertPollInterval
	}
	interval, err := time.ParseDuration(intervalStr)
	if err != nil {
		fmt.Println("Invalid interval")
		return
	}
	if err := st.AlertWatcher.WatchWithAlert(st.GetDriver(), st.GetPID(), vt, cfg, interval); err != nil {
		fmt.Printf("Alert failed: %v\n", err)
		return
	}
	fmt.Printf("Alert set on 0x%x: condition=%s action=%s\n", addr, cond, action)
}

func RemoveAlert(st *app.State) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := st.AlertWatcher.RemoveAlert(addr); err != nil {
		fmt.Printf("Remove alert: %v\n", err)
	} else {
		fmt.Printf("Alert removed for 0x%x\n", addr)
	}
}
