package cli

import (
	"fmt"
	"strconv"

	"memdroid/internal/driver/adb"
)

func SelectDevice(d *adb.ADB) {
	devices, err := d.ListDevices()
	if err != nil {
		fmt.Printf("Failed to list devices: %v\n", err)
		return
	}
	if len(devices) == 0 {
		fmt.Println("No devices connected")
		return
	}
	fmt.Println("Connected devices:")
	for i, s := range devices {
		fmt.Printf("  %d. %s\n", i+1, s)
	}
	idx, err := strconv.Atoi(Prompt("Select device number: "))
	if err != nil || idx < 1 || idx > len(devices) {
		fmt.Println("Invalid selection")
		return
	}
	serial := devices[idx-1]
	if err := d.SelectDevice(serial); err != nil {
		fmt.Printf("Select device failed: %v\n", err)
		return
	}
	fmt.Printf("Using device: %s\n", serial)
}

func ConnectWifi(d *adb.ADB) {
	addr := Prompt("Host:port (e.g. 192.168.1.5:5555): ")
	if err := d.ConnectWifi(addr); err != nil {
		fmt.Printf("Connect failed: %v\n", err)
		return
	}
	fmt.Printf("Connected to %s\n", addr)
}

func DisconnectWifi(d *adb.ADB) {
	addr := Prompt("Host:port to disconnect: ")
	if err := d.DisconnectWifi(addr); err != nil {
		fmt.Printf("Disconnect failed: %v\n", err)
		return
	}
	fmt.Printf("Disconnected from %s\n", addr)
}
