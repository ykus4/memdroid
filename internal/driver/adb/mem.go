package adb

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// Peek reads size bytes from addr in /proc/<pid>/mem via adb exec-out + dd.
// The output is piped through base64 to survive adb's text transport.
func (a *ADB) Peek(pid int, addr uintptr, size int) ([]byte, error) {
	// base64 wrapping avoids binary corruption over adb shell text mode.
	cmd := fmt.Sprintf(
		"su -c 'dd if=/proc/%d/mem bs=1 skip=%d count=%d 2>/dev/null | base64'",
		pid, addr, size,
	)
	out, err := a.shell("sh", "-c", cmd)
	if err != nil {
		return nil, fmt.Errorf("peek 0x%x: %w", addr, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		return nil, fmt.Errorf("peek decode 0x%x: %w", addr, err)
	}
	if len(decoded) != size {
		return nil, fmt.Errorf("peek 0x%x: wanted %d bytes, got %d", addr, size, len(decoded))
	}
	return decoded, nil
}

// Poke writes data to addr in /proc/<pid>/mem via adb exec-out + dd.
// The payload is transmitted as base64 and decoded on-device.
func (a *ADB) Poke(pid int, addr uintptr, data []byte) error {
	b64 := base64.StdEncoding.EncodeToString(data)
	cmd := fmt.Sprintf(
		"echo %s | base64 -d | dd of=/proc/%d/mem bs=1 seek=%d count=%d conv=notrunc 2>/dev/null",
		b64, pid, addr, len(data),
	)
	_, err := a.shellRoot(cmd)
	if err != nil {
		return fmt.Errorf("poke 0x%x: %w", addr, err)
	}
	return nil
}

// ReadBytes reads exactly n bytes starting at addr using repeated Peek calls
// in word-sized chunks to minimise round-trips.
func (a *ADB) ReadBytes(pid int, addr uintptr, n int) ([]byte, error) {
	const chunkSize = 256 // balance round-trip count vs. dd overhead
	out := make([]byte, 0, n)
	for len(out) < n {
		rem := n - len(out)
		if rem > chunkSize {
			rem = chunkSize
		}
		b, err := a.Peek(pid, addr+uintptr(len(out)), rem)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

// ReadRegion reads the entire region [addr, addr+size) with as few adb shell
// round-trips as possible. Large regions are split into maxRegionChunk-byte
// pieces so that the base64 payload stays within shell buffer limits.
func (a *ADB) ReadRegion(pid int, addr uintptr, size int) ([]byte, error) {
	const maxRegionChunk = 32 * 1024 * 1024 // 32 MB per adb call
	out := make([]byte, 0, size)
	for len(out) < size {
		rem := size - len(out)
		if rem > maxRegionChunk {
			rem = maxRegionChunk
		}
		cur := addr + uintptr(len(out))
		cmd := fmt.Sprintf(
			"su -c 'dd if=/proc/%d/mem bs=4096 skip=%d count=%d iflag=skip_bytes,count_bytes 2>/dev/null | base64'",
			pid, cur, rem,
		)
		raw, err := a.shell("sh", "-c", cmd)
		if err != nil {
			return nil, fmt.Errorf("read region 0x%x+%d: %w", cur, rem, err)
		}
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil {
			return nil, fmt.Errorf("read region decode 0x%x: %w", cur, err)
		}
		if len(decoded) == 0 {
			// Region is not readable (e.g. guard page); treat as error.
			return nil, fmt.Errorf("read region 0x%x: empty read", cur)
		}
		out = append(out, decoded...)
		if len(decoded) < rem {
			// Short read — reached end of readable area.
			break
		}
	}
	return out, nil
}
