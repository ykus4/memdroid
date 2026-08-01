package main

import (
	"flag"
	"fmt"
	"os"

	"memdroid/internal/app"
	"memdroid/internal/cli"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/watch"
	"memdroid/internal/server"
)

func main() {
	if err := run(); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "memdroid:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := flag.String("addr", "127.0.0.1:8080", "HTTP listen address for the Web UI/API")
	token := flag.String("token", os.Getenv("MEMDROID_TOKEN"), "require this auth token on /api and /ws (empty = no auth)")
	fileRoot := flag.String("file-root", ".", "confine API file reads/writes to this directory (empty = unrestricted)")
	flag.Parse()

	d := adb.New()
	autoSelectDevice(d)

	st := app.NewState(d)
	installCLIWatchOutput(st)

	if !server.IsLoopback(*addr) && *token == "" {
		_, _ = fmt.Fprintf(os.Stderr,
			"WARNING: binding %s exposes root memory access to the network with no auth. Use -addr 127.0.0.1:PORT or set -token.\n",
			*addr)
	}

	srv, err := server.New(server.Config{
		Addr:     *addr,
		Token:    *token,
		FileRoot: *fileRoot,
	}, st, d)
	if err != nil {
		return err
	}

	fmt.Printf("Web UI: %s\n", server.DisplayURL(*addr))
	if *token != "" {
		fmt.Printf("Auth token required — open %s/?token=<token>\n", server.DisplayURL(*addr))
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil {
			_, _ = fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		}
	}()

	cli.ServerURL = server.DisplayURL(*addr)
	cli.Run(st, d)

	return srv.Shutdown()
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

// installCLIWatchOutput prints watch and alert activity to the terminal. These
// are registrations rather than assignments, so the server's WebSocket
// forwarding coexists with them instead of replacing them.
func installCLIWatchOutput(st *app.State) {
	st.Watcher.OnChange(func(ev watch.ChangeEvent) {
		fmt.Printf("[Watch] 0x%x: %s -> %s\n", ev.Addr, ev.Prev, ev.Cur)
	})
	st.AlertWatcher.OnAlert(func(ev watch.AlertEvent) {
		action := "notify"
		if ev.Triggered {
			action = "WRITE"
		}
		fmt.Printf("[Alert] 0x%x: condition=%s value=%s action=%s\n", ev.Addr, ev.Condition, ev.Value, action)
	})
}
