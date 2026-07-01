package main

import (
	"fmt"
	"os"

	"memodroid/internal/app"
	"memodroid/internal/cli"
	"memodroid/internal/driver/adb"
	"memodroid/internal/memory/watch"
	"memodroid/internal/server"
)

const defaultServerAddr = ":8080"

func main() {
	d := adb.New()
	autoSelectDevice(d)

	st := app.NewState(d)
	installWatchHandlers(st)

	go func() {
		if err := server.Start(defaultServerAddr, st, d); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		}
	}()

	cli.ServerAddr = defaultServerAddr
	cli.Run(st, d)
}

func autoSelectDevice(d *adb.ADB) {
	devices, err := d.ListDevices()
	if err != nil {
		return
	}
	switch len(devices) {
	case 0:
		fmt.Println("Warning: no ADB devices connected.")
	case 1:
		if err := d.SelectDevice(devices[0]); err == nil {
			fmt.Printf("Auto-selected device: %s\n", devices[0])
		}
	default:
		cli.SelectDevice(d)
	}
}

func installWatchHandlers(st *app.State) {
	st.Watcher.OnChange = func(ev watch.ChangeEvent) {
		fmt.Printf("[Watch] 0x%x: %s -> %s\n", ev.Addr, ev.Prev, ev.Cur)
	}
	st.AlertWatcher.OnAlert = func(ev watch.AlertEvent) {
		action := "notify"
		if ev.Triggered {
			action = "WRITE"
		}
		fmt.Printf("[Alert] 0x%x: condition=%s value=%s action=%s\n", ev.Addr, ev.Condition, ev.Value, action)
	}
}
