package cli

import (
	"fmt"

	"memodroid/internal/app"
	"memodroid/internal/driver"
	"memodroid/internal/memory/search"
)

// MaxCandidatesDisplay caps how many search candidates are printed.
const MaxCandidatesDisplay = 50

func SetValueType(st *app.State) {
	fmt.Println("Types: 1=int32  2=int64  3=float32  4=float64  5=uint32  6=uint64  7=bytes")
	var vt search.ValueType
	switch Prompt("Type: ") {
	case "1":
		vt = search.TypeInt32
	case "2":
		vt = search.TypeInt64
	case "3":
		vt = search.TypeFloat32
	case "4":
		vt = search.TypeFloat64
	case "5":
		vt = search.TypeUint32
	case "6":
		vt = search.TypeUint64
	case "7":
		vt = search.TypeBytes
	default:
		fmt.Println("Invalid type")
		return
	}
	st.SetValueType(vt)
	if sess := st.GetSession(); sess != nil {
		st.SetSession(search.NewSession(st.GetPID(), vt, st.GetDriver()))
		fmt.Println("Search session reset for new type")
	}
}

func SearchFiltered(st *app.State) {
	fmt.Println("Region: 1=all  2=heap  3=stack  4=anon  5=custom range")
	var filter driver.RegionFilter
	var customStart, customEnd uintptr
	switch Prompt("Region: ") {
	case "2":
		filter = driver.RegionHeap
	case "3":
		filter = driver.RegionStack
	case "4":
		filter = driver.RegionAnon
	case "5":
		filter = driver.RegionCustom
		var ok bool
		if customStart, ok = ParseAddr("Start address (hex): "); !ok {
			return
		}
		if customEnd, ok = ParseAddr("End address (hex): "); !ok {
			return
		}
	default:
		filter = driver.RegionAll
	}
	val, ok := ParseValue("Value: ", st.GetValueType())
	if !ok {
		return
	}
	if err := st.EnsureSession().SearchFiltered(val, filter, customStart, customEnd); err != nil {
		fmt.Printf("Search failed: %v\n", err)
	}
}

func ShowCandidates(st *app.State) {
	sess := st.GetSession()
	vt := st.GetValueType()
	candidates := sess.Snapshot()
	if len(candidates) == 0 {
		fmt.Println("No candidates")
		return
	}
	shown := 0
	for addr, val := range candidates {
		fmt.Printf("  0x%x = %s\n", addr, search.FormatValue(val, vt))
		shown++
		if shown >= MaxCandidatesDisplay {
			fmt.Printf("  ... (%d total)\n", len(candidates))
			break
		}
	}
}
