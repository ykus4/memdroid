package search

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ValueType identifies the numeric type used for memory searches.
type ValueType int

const (
	TypeInt32 ValueType = iota
	TypeInt64
	TypeFloat32
	TypeFloat64
	TypeUint32
	TypeUint64
	TypeBytes // arbitrary byte sequence
)

// valueTypeNames is the single source of truth for the ValueType <-> string
// mapping used by String, ParseValueType and ValueTypeNames.
var valueTypeNames = []struct {
	t    ValueType
	name string
}{
	{TypeInt32, "int32"},
	{TypeInt64, "int64"},
	{TypeFloat32, "float32"},
	{TypeFloat64, "float64"},
	{TypeUint32, "uint32"},
	{TypeUint64, "uint64"},
	{TypeBytes, "bytes"},
}

func (t ValueType) String() string {
	for _, e := range valueTypeNames {
		if e.t == t {
			return e.name
		}
	}
	return "unknown"
}

// ParseValueType converts a string name to ValueType.
func ParseValueType(s string) (ValueType, error) {
	for _, e := range valueTypeNames {
		if e.name == s {
			return e.t, nil
		}
	}
	return 0, fmt.Errorf("unknown value type %q", s)
}

// ValueTypeNames returns every supported type name, in declaration order.
func ValueTypeNames() []string {
	out := make([]string, len(valueTypeNames))
	for i, e := range valueTypeNames {
		out[i] = e.name
	}
	return out
}

// Size returns the byte size of the type. For TypeBytes returns 0 (variable).
func (t ValueType) Size() int {
	switch t {
	case TypeInt32, TypeFloat32, TypeUint32:
		return 4
	case TypeInt64, TypeFloat64, TypeUint64:
		return 8
	}
	return 0
}

// IsFixedSize reports whether the type has a known fixed byte width.
func (t ValueType) IsFixedSize() bool {
	return t.Size() != 0
}

// ParseValue converts a string to little-endian bytes for the given type.
// For TypeBytes, accepts hex string like "FF 00 AB" or "FF00AB".
func ParseValue(s string, t ValueType) ([]byte, error) {
	if t == TypeBytes {
		return parseHexBytes(s)
	}
	buf := make([]byte, t.Size())
	switch t {
	case TypeInt32:
		v, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, uint32(int32(v)))
	case TypeInt64:
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, uint64(v))
	case TypeFloat32:
		v, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, math.Float32bits(float32(v)))
	case TypeFloat64:
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
	case TypeUint32:
		v, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint32(buf, uint32(v))
	case TypeUint64:
		v, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return nil, err
		}
		binary.LittleEndian.PutUint64(buf, v)
	default:
		return nil, fmt.Errorf("unknown value type %d", t)
	}
	return buf, nil
}

// FormatValue converts little-endian bytes back to a human-readable string.
// Slices shorter than the type's fixed width render as "?" rather than
// panicking, since truncated reads are normal at region boundaries.
func FormatValue(b []byte, t ValueType) string {
	if t == TypeBytes {
		return hex.EncodeToString(b)
	}
	if len(b) < t.Size() {
		return "?"
	}
	switch t {
	case TypeInt32:
		return strconv.FormatInt(int64(int32(binary.LittleEndian.Uint32(b))), 10)
	case TypeInt64:
		return strconv.FormatInt(int64(binary.LittleEndian.Uint64(b)), 10)
	case TypeFloat32:
		return strconv.FormatFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(b))), 'g', -1, 32)
	case TypeFloat64:
		return strconv.FormatFloat(math.Float64frombits(binary.LittleEndian.Uint64(b)), 'g', -1, 64)
	case TypeUint32:
		return strconv.FormatUint(uint64(binary.LittleEndian.Uint32(b)), 10)
	case TypeUint64:
		return strconv.FormatUint(binary.LittleEndian.Uint64(b), 10)
	}
	return "?"
}

// CompareValues returns -1, 0, or 1. It returns 0 for TypeBytes and for
// slices shorter than the type's fixed width (guarding against panics on
// truncated reads).
func CompareValues(a, b []byte, t ValueType) int {
	size := t.Size()
	if size == 0 || len(a) < size || len(b) < size {
		return 0
	}
	switch t {
	case TypeInt32:
		return cmp.Compare(int32(binary.LittleEndian.Uint32(a)), int32(binary.LittleEndian.Uint32(b)))
	case TypeInt64:
		return cmp.Compare(int64(binary.LittleEndian.Uint64(a)), int64(binary.LittleEndian.Uint64(b)))
	case TypeFloat32:
		return cmp.Compare(math.Float32frombits(binary.LittleEndian.Uint32(a)), math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case TypeFloat64:
		return cmp.Compare(math.Float64frombits(binary.LittleEndian.Uint64(a)), math.Float64frombits(binary.LittleEndian.Uint64(b)))
	case TypeUint32:
		return cmp.Compare(binary.LittleEndian.Uint32(a), binary.LittleEndian.Uint32(b))
	case TypeUint64:
		return cmp.Compare(binary.LittleEndian.Uint64(a), binary.LittleEndian.Uint64(b))
	}
	return 0
}

// EqualBytes reports whether a and b contain identical bytes. It is a thin
// alias for bytes.Equal, kept because callers outside this package read better
// with a search-domain name alongside CompareValues.
func EqualBytes(a, b []byte) bool { return bytes.Equal(a, b) }

// filterModeNames is the single source of truth for the FilterMode <-> string
// mapping.
var filterModeNames = []struct {
	m    FilterMode
	name string
}{
	{FilterChanged, "changed"},
	{FilterUnchanged, "unchanged"},
	{FilterIncreased, "increased"},
	{FilterDecreased, "decreased"},
	{FilterValue, "value"},
}

// ParseFilterMode converts a string name to FilterMode.
func ParseFilterMode(s string) (FilterMode, error) {
	for _, e := range filterModeNames {
		if e.name == s {
			return e.m, nil
		}
	}
	return 0, fmt.Errorf("unknown filter mode %q", s)
}

func (m FilterMode) String() string {
	for _, e := range filterModeNames {
		if e.m == m {
			return e.name
		}
	}
	return "unknown"
}

// parseHexBytes parses "FF 00 AB" or "FF00AB" into bytes.
func parseHexBytes(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, " ", "")
	b, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid hex bytes %q: %w", s, err)
	}
	if len(b) == 0 {
		return nil, fmt.Errorf("empty byte sequence")
	}
	return b, nil
}
