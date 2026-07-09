package adb

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"memdroid/internal/driver"
)

// ReadMaps returns all rw memory regions for pid.
func (a *ADB) ReadMaps(pid int) ([]driver.Region, error) {
	return a.ReadMapsFiltered(pid, driver.RegionAll, 0, 0)
}

// ReadMapsFiltered returns memory regions matching filter.
func (a *ADB) ReadMapsFiltered(pid int, filter driver.RegionFilter, customStart, customEnd uintptr) ([]driver.Region, error) {
	out, err := a.shellRoot(fmt.Sprintf("cat /proc/%d/maps", pid))
	if err != nil {
		return nil, fmt.Errorf("read maps pid %d: %w", pid, err)
	}

	var regions []driver.Region
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		r, ok := parseMapsLine(scanner.Text())
		if ok && driver.MatchFilter(r, filter, customStart, customEnd) {
			regions = append(regions, r)
		}
	}
	return regions, scanner.Err()
}

func parseMapsLine(line string) (driver.Region, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return driver.Region{}, false
	}

	var startHex, endHex uint64
	if n, _ := fmt.Sscanf(fields[0], "%x-%x", &startHex, &endHex); n != 2 {
		return driver.Region{}, false
	}

	perms := fields[1]
	if len(perms) < 2 || perms[0] != 'r' || perms[1] != 'w' {
		return driver.Region{}, false
	}

	name := ""
	if len(fields) >= 6 {
		name = fields[5]
	}
	return driver.Region{Start: uintptr(startHex), End: uintptr(endHex), Name: name}, true
}
