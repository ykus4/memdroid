package search

import (
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

func (t ValueType) String() string {
	switch t {
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeFloat32:
		return "float32"
	case TypeFloat64:
		return "float64"
	case TypeUint32:
		return "uint32"
	case TypeUint64:
		return "uint64"
	case TypeBytes:
		return "bytes"
	}
	return "unknown"
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
	return t != TypeBytes
}

// ParseValue converts a string to little-endian bytes for the given type.
// For TypeBytes, accepts hex string like "FF 00 AB" or "FF00AB".
func ParseValue(s string, t ValueType) ([]byte, error) {
	switch t {
	case TypeBytes:
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
func FormatValue(b []byte, t ValueType) string {
	switch t {
	case TypeInt32:
		if len(b) < 4 {
			return "?"
		}
		return fmt.Sprintf("%d", int32(binary.LittleEndian.Uint32(b)))
	case TypeInt64:
		if len(b) < 8 {
			return "?"
		}
		return fmt.Sprintf("%d", int64(binary.LittleEndian.Uint64(b)))
	case TypeFloat32:
		if len(b) < 4 {
			return "?"
		}
		return fmt.Sprintf("%g", math.Float32frombits(binary.LittleEndian.Uint32(b)))
	case TypeFloat64:
		if len(b) < 8 {
			return "?"
		}
		return fmt.Sprintf("%g", math.Float64frombits(binary.LittleEndian.Uint64(b)))
	case TypeUint32:
		if len(b) < 4 {
			return "?"
		}
		return fmt.Sprintf("%d", binary.LittleEndian.Uint32(b))
	case TypeUint64:
		if len(b) < 8 {
			return "?"
		}
		return fmt.Sprintf("%d", binary.LittleEndian.Uint64(b))
	case TypeBytes:
		return hex.EncodeToString(b)
	}
	return "?"
}

// CompareValues returns -1, 0, or 1. Not defined for TypeBytes.
func CompareValues(a, b []byte, t ValueType) int {
	switch t {
	case TypeInt32:
		return cmpInt(int64(int32(binary.LittleEndian.Uint32(a))),
			int64(int32(binary.LittleEndian.Uint32(b))))
	case TypeInt64:
		return cmpInt(int64(binary.LittleEndian.Uint64(a)),
			int64(binary.LittleEndian.Uint64(b)))
	case TypeFloat32:
		return cmpFloat(float64(math.Float32frombits(binary.LittleEndian.Uint32(a))),
			float64(math.Float32frombits(binary.LittleEndian.Uint32(b))))
	case TypeFloat64:
		return cmpFloat(math.Float64frombits(binary.LittleEndian.Uint64(a)),
			math.Float64frombits(binary.LittleEndian.Uint64(b)))
	case TypeUint32:
		return cmpUint(uint64(binary.LittleEndian.Uint32(a)),
			uint64(binary.LittleEndian.Uint32(b)))
	case TypeUint64:
		return cmpUint(binary.LittleEndian.Uint64(a),
			binary.LittleEndian.Uint64(b))
	}
	return 0
}

func cmpInt(a, b int64) int {
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

func cmpUint(a, b uint64) int {
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

func cmpFloat(a, b float64) int {
	if a < b {
		return -1
	} else if a > b {
		return 1
	}
	return 0
}

// EqualBytes reports whether a and b contain identical bytes.
func EqualBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ParseValueType converts a string name to ValueType.
func ParseValueType(s string) (ValueType, error) {
	switch s {
	case "int32":
		return TypeInt32, nil
	case "int64":
		return TypeInt64, nil
	case "float32":
		return TypeFloat32, nil
	case "float64":
		return TypeFloat64, nil
	case "uint32":
		return TypeUint32, nil
	case "uint64":
		return TypeUint64, nil
	case "bytes":
		return TypeBytes, nil
	}
	return 0, fmt.Errorf("unknown value type %q", s)
}

// ParseFilterMode converts a string name to FilterMode.
func ParseFilterMode(s string) (FilterMode, error) {
	switch s {
	case "changed":
		return FilterChanged, nil
	case "unchanged":
		return FilterUnchanged, nil
	case "increased":
		return FilterIncreased, nil
	case "decreased":
		return FilterDecreased, nil
	case "value":
		return FilterValue, nil
	}
	return 0, fmt.Errorf("unknown filter mode %q", s)
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
