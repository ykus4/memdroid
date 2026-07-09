package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"memdroid/internal/app"
	"memdroid/internal/cli"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/watch"
	"memdroid/internal/server"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address for the Web UI/API")
	token := flag.String("token", os.Getenv("MEMDROID_TOKEN"), "require this auth token on /api and /ws (empty = no auth)")
	flag.Parse()

	d := adb.New()
	autoSelectDevice(d)

	st := app.NewState(d)
	installWatchHandlers(st)

	if !isLoopback(*addr) && *token == "" {
		_, _ = fmt.Fprintf(os.Stderr,
			"WARNING: binding %s exposes root memory access to the network with no auth. Use -addr 127.0.0.1:PORT or set -token.\n",
			*addr)
	}

	go func() {
		if err := server.Start(*addr, *token, st, d); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		}
	}()

	cli.ServerURL = server.DisplayURL(*addr)
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

func isLoopback(addr string) bool {
	host, _, found := strings.Cut(addr, ":")
	if !found {
		host = addr
	}
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}
