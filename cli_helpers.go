package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"memodroid/internal/memory/search"
)

var stdinReader = bufio.NewReader(os.Stdin)

func prompt(label string) string {
	fmt.Print(label)
	s, _ := stdinReader.ReadString('\n')
	return strings.TrimSpace(s)
}

func parseAddr(label string) (uintptr, bool) {
	v, err := strconv.ParseUint(prompt(label), 16, 64)
	if err != nil {
		fmt.Println("Invalid address")
		return 0, false
	}
	return uintptr(v), true
}

func parseValue(label string, vt search.ValueType) ([]byte, bool) {
	val, err := search.ParseValue(prompt(label), vt)
	if err != nil {
		fmt.Println("Invalid value")
		return nil, false
	}
	return val, true
}

func requireAttached(pid int) bool {
	if pid == 0 {
		fmt.Println("No process attached")
		return false
	}
	return true
}

func requireSession(s *search.Session) bool {
	if s == nil || !s.HasCandidates() {
		fmt.Println("No active search session. Run Search first.")
		return false
	}
	return true
}
