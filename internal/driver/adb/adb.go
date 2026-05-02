// Package adb implements the driver.Driver interface over Android Debug Bridge.
// All operations run as root on the device via "adb shell su -c ...".
package adb

import (
	"fmt"
	"os/exec"
	"strings"
	"sync"

	"memodroid/internal/driver"
)

// ADB implements driver.Driver using the adb CLI.
type ADB struct {
	mu     sync.RWMutex
	serial string // empty = use the single connected device
}

// New returns a new ADB driver. Call SelectDevice before use when multiple
// devices are connected.
func New() *ADB {
	return &ADB{}
}

// --- device helpers ---

func (a *ADB) DeviceSerial() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.serial
}

func (a *ADB) SelectDevice(serial string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.serial = serial
	return nil
}

// ConnectWifi connects to a device over TCP/IP (Wi-Fi ADB).
// addr is "host:port", e.g. "192.168.1.5:5555".
func (a *ADB) ConnectWifi(addr string) error {
	out, err := exec.Command("adb", "connect", addr).Output()
	if err != nil {
		return fmt.Errorf("adb connect %s: %w", addr, err)
	}
	result := strings.TrimSpace(string(out))
	if strings.HasPrefix(result, "failed") || strings.HasPrefix(result, "error") {
		return fmt.Errorf("adb connect: %s", result)
	}
	// Auto-select the newly connected device.
	a.mu.Lock()
	a.serial = addr
	a.mu.Unlock()
	return nil
}

// DisconnectWifi disconnects a TCP/IP device.
func (a *ADB) DisconnectWifi(addr string) error {
	_, err := exec.Command("adb", "disconnect", addr).Output()
	if err != nil {
		return fmt.Errorf("adb disconnect %s: %w", addr, err)
	}
	a.mu.Lock()
	if a.serial == addr {
		a.serial = ""
	}
	a.mu.Unlock()
	return nil
}

// ListDevices returns the serial numbers of all connected devices/emulators.
func (a *ADB) ListDevices() ([]string, error) {
	out, err := exec.Command("adb", "devices").Output()
	if err != nil {
		return nil, fmt.Errorf("adb devices: %w", err)
	}
	var serials []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			serials = append(serials, fields[0])
		}
	}
	return serials, nil
}

// shell runs "adb [-s serial] shell <args...>" and returns combined output.
func (a *ADB) shell(args ...string) ([]byte, error) {
	cmdArgs := a.baseArgs("shell")
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.Command("adb", cmdArgs...).Output()
	if err != nil {
		return nil, fmt.Errorf("adb shell %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// shellRoot runs a command as root: adb shell su -c '<cmd>'.
func (a *ADB) shellRoot(cmd string) ([]byte, error) {
	return a.shell("su", "-c", cmd)
}

// baseArgs prepends [-s serial] when a device is selected.
func (a *ADB) baseArgs(verb string) []string {
	a.mu.RLock()
	s := a.serial
	a.mu.RUnlock()
	if s != "" {
		return []string{"-s", s, verb}
	}
	return []string{verb}
}

// Compile-time check.
var _ driver.Driver = (*ADB)(nil)
