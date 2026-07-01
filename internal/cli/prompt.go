package cli

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"memodroid/internal/memory/search"
)

var stdinReader = bufio.NewReader(os.Stdin)

// Prompt writes label to stdout and returns the trimmed user input.
func Prompt(label string) string {
	fmt.Print(label)
	s, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(s)
}

// ParseAddr prompts for a hexadecimal address.
func ParseAddr(label string) (uintptr, bool) {
	v, err := strconv.ParseUint(Prompt(label), 16, 64)
	if err != nil {
		fmt.Println("Invalid address")
		return 0, false
	}
	return uintptr(v), true
}

// ParseValue prompts for a typed value and parses it.
func ParseValue(label string, vt search.ValueType) ([]byte, bool) {
	val, err := search.ParseValue(Prompt(label), vt)
	if err != nil {
		fmt.Println("Invalid value")
		return nil, false
	}
	return val, true
}

// RequireAttached prints an error and returns false when no process is attached.
func RequireAttached(pid int) bool {
	if pid == 0 {
		fmt.Println("No process attached")
		return false
	}
	return true
}

// RequireSession prints an error and returns false when there is no active search session.
func RequireSession(s *search.Session) bool {
	if s == nil || !s.HasCandidates() {
		fmt.Println("No active search session. Run Search first.")
		return false
	}
	return true
}
