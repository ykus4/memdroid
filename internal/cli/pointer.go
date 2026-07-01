package cli

import (
	"fmt"
	"strconv"
	"strings"

	"memodroid/internal/app"
	"memodroid/internal/memory/pointer"
)

func PointerScan(st *app.State) {
	addr, ok := ParseAddr("Target address (hex): ")
	if !ok {
		return
	}
	depthStr := Prompt("Max depth [default: 5]: ")
	maxDepth := pointer.DefaultMaxDepth
	if depthStr != "" {
		if v, err := strconv.Atoi(depthStr); err == nil && v > 0 {
			maxDepth = v
		}
	}
	offsetStr := Prompt(fmt.Sprintf("Max offset [default: 0x%x]: ", pointer.DefaultMaxOffset))
	maxOffset := uintptr(pointer.DefaultMaxOffset)
	if offsetStr != "" {
		if v, err := strconv.ParseUint(offsetStr, 0, 64); err == nil {
			maxOffset = uintptr(v)
		}
	}
	fmt.Println("Scanning... (this may take a while)")
	result, err := pointer.Scan(st.GetDriver(), st.GetPID(), addr, maxDepth, maxOffset)
	if err != nil {
		fmt.Printf("Pointer scan failed: %v\n", err)
		return
	}
	if len(result.Chains) == 0 {
		fmt.Println("No pointer chains found")
		return
	}
	fmt.Printf("Found %d chains:\n", len(result.Chains))
	for i, c := range result.Chains {
		fmt.Printf("  [%d] %s\n", i+1, pointer.FormatChain(c))
	}
}

func PointerResolve(st *app.State) {
	label := Prompt("Module name (e.g. libil2cpp.so): ")
	if label == "" {
		fmt.Println("Module name required")
		return
	}
	offsetsStr := Prompt("Offsets (comma-separated hex, e.g. 0x10,0x20,0x8): ")
	if offsetsStr == "" {
		fmt.Println("Offsets required")
		return
	}
	offsets, err := parseOffsetList(offsetsStr)
	if err != nil {
		fmt.Println(err)
		return
	}
	chain := pointer.Chain{
		BaseLabel: label,
		Offsets:   offsets,
	}
	resolved, err := pointer.ResolveChain(st.GetDriver(), st.GetPID(), chain)
	if err != nil {
		fmt.Printf("Resolve failed: %v\n", err)
		return
	}
	fmt.Printf("Resolved address: 0x%x\n", resolved)
}

func parseOffsetList(s string) ([]int64, error) {
	parts := strings.Split(s, ",")
	offsets := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		v, err := strconv.ParseInt(p, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid offset %q: %w", p, err)
		}
		offsets = append(offsets, v)
	}
	return offsets, nil
}
