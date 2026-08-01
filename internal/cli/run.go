package cli

import (
	"fmt"
	"strconv"

	"memdroid/internal/app"
	"memdroid/internal/driver"
	"memdroid/internal/driver/adb"
	"memdroid/internal/memory/modify"
	"memdroid/internal/memory/search"
	"memdroid/internal/memory/store"
)

// exitKey leaves the menu loop. It is handled by Run rather than the menu table
// because it is the one action that ends the program.
const exitKey = "0"

// Run drives the interactive menu loop until the user selects Exit, then
// releases the target process.
func Run(st *app.State, d *adb.ADB) {
	defer shutdown(st)
	for {
		PrintMenu(st, d)
		if !dispatch(st, d, Prompt("")) {
			return
		}
	}
}

// dispatch handles a single menu choice. Returns false when the user chose Exit.
func dispatch(st *app.State, d *adb.ADB, choice string) bool {
	if choice == exitKey {
		return false
	}
	fn, ok := actions[choice]
	if !ok {
		fmt.Println("Invalid choice")
		return true
	}
	fn(newCmd(st, d))
	return true
}

// shutdown releases every resource held on the device before exiting.
func shutdown(st *app.State) {
	st.Freezer.UnfreezeAll()
	st.Watcher.UnwatchAll()
	st.AlertWatcher.RemoveAll()
	if pid := st.GetPID(); pid != 0 {
		st.GetDriver().Detach(pid)
	}
	fmt.Println("Bye!")
}

// report prints the outcome of an operation that only reports failure.
func report(op string, err error) {
	if err != nil {
		fmt.Printf("%s failed: %v\n", op, err)
	}
}

// --- process ---

func listProcesses(drv driver.Driver) {
	procs, err := drv.ListProcesses()
	if err != nil {
		fmt.Printf("List failed: %v\n", err)
		return
	}
	for _, p := range procs {
		fmt.Printf("PID: %5d  Name: %s\n", p.PID, p.Name)
	}
}

// --- search ---

func searchValue(c *cmd) {
	val, ok := ParseValue("Value: ", c.vt)
	if !ok {
		return
	}
	sess := c.st.EnsureSession()
	if err := sess.Search(val); err != nil {
		fmt.Printf("Search failed: %v\n", err)
		return
	}
	fmt.Printf("Found %d addresses\n", sess.CandidateCount())
}

// filterAction builds the action for one of the no-argument filter modes.
func filterAction(mode search.FilterMode) action {
	return func(c *cmd) {
		if err := c.sess.Filter(mode, nil); err != nil {
			fmt.Printf("Filter failed: %v\n", err)
			return
		}
		fmt.Printf("Remaining: %d addresses\n", c.sess.CandidateCount())
	}
}

func filterByValue(c *cmd) {
	val, ok := ParseValue("Value: ", c.vt)
	if !ok {
		return
	}
	if err := c.sess.Filter(search.FilterValue, val); err != nil {
		fmt.Printf("Filter failed: %v\n", err)
		return
	}
	fmt.Printf("Remaining: %d addresses\n", c.sess.CandidateCount())
}

func resetSearch(c *cmd) {
	if c.sess != nil {
		c.sess.Reset()
	}
	fmt.Println("Search session reset")
}

func searchByPattern(c *cmd) {
	pat, err := search.ParsePattern(Prompt("Pattern (e.g. FF 00 ?? 01): "))
	if err != nil {
		fmt.Printf("Invalid pattern: %v\n", err)
		return
	}
	result, err := search.SearchPattern(c.drv, c.pid, pat)
	if err != nil {
		fmt.Printf("Pattern search failed: %v\n", err)
		return
	}
	printScanResult(result)
}

func searchByString(enc search.StringEncoding) action {
	label := "String (UTF-8): "
	if enc == search.EncodingUTF16LE {
		label = "String (UTF-16LE): "
	}
	return func(c *cmd) {
		result, err := search.SearchString(c.drv, c.pid, Prompt(label), enc)
		if err != nil {
			fmt.Printf("Search failed: %v\n", err)
			return
		}
		printScanResult(result)
	}
}

func printScanResult(result search.PatternResult) {
	for _, m := range result.Matches {
		fmt.Printf("  Found at 0x%x\n", m.Addr)
	}
	fmt.Printf("Total: %d results\n", len(result.Matches))
	if result.Truncated {
		fmt.Printf("(stopped at the %d-result limit — narrow the pattern for more)\n", search.PatternMaxResults)
	}
}

// --- memory ---

func modifyStringAt(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := modify.WriteString(c.drv, c.pid, addr, Prompt("New string (UTF-8): ")); err != nil {
		fmt.Printf("Modify failed: %v\n", err)
		return
	}
	fmt.Println("Modified successfully")
}

func modifyAt(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	val, ok := ParseValue("New value: ", c.vt)
	if !ok {
		return
	}
	if err := c.st.UndoStack.WithUndo(c.drv, c.pid, addr, val); err != nil {
		fmt.Printf("Modify failed: %v\n", err)
		return
	}
	fmt.Printf("Modified 0x%x (undo available, depth: %d)\n", addr, c.st.UndoStack.Depth())
}

func undoLast(c *cmd) {
	if err := c.st.UndoStack.Undo(); err != nil {
		fmt.Printf("Undo: %v\n", err)
		return
	}
	fmt.Printf("Undone (remaining depth: %d)\n", c.st.UndoStack.Depth())
}

func freezeAt(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	val, ok := ParseValue("Value to freeze: ", c.vt)
	if !ok {
		return
	}
	if err := c.st.Freezer.Freeze(c.drv, c.pid, addr, val); err != nil {
		fmt.Printf("Freeze failed: %v\n", err)
		return
	}
	fmt.Printf("Freezing 0x%x\n", addr)
}

func freezeAllCandidates(c *cmd) {
	count := c.st.Freezer.FreezeAllCandidates(c.drv, c.sess)
	fmt.Printf("Freezing %d addresses\n", count)
}

func unfreezeAt(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := c.st.Freezer.Unfreeze(addr); err != nil {
		fmt.Printf("Unfreeze: %v\n", err)
		return
	}
	fmt.Printf("Unfrozen 0x%x\n", addr)
}

func unwatchAt(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	if err := c.st.Watcher.Unwatch(addr); err != nil {
		fmt.Printf("Unwatch: %v\n", err)
		return
	}
	fmt.Printf("Stopped watching 0x%x\n", addr)
}

// --- bookmarks ---

func addBookmark(c *cmd) {
	addr, ok := ParseAddr("Address (hex): ")
	if !ok {
		return
	}
	c.st.GetBookmarks().Add(addr, Prompt("Label: "), c.vt)
	fmt.Printf("Bookmarked 0x%x\n", addr)
}

func modifyAllBookmarks(c *cmd) {
	val, ok := ParseValue("Value: ", c.vt)
	if !ok {
		return
	}
	count := c.st.GetBookmarks().ModifyAll(c.drv, c.pid, val, c.vt)
	fmt.Printf("Modified %d bookmarks\n", count)
}

func removeBookmark(c *cmd) {
	BookmarkList(c.st)
	idx, err := strconv.Atoi(Prompt("Index to remove: "))
	if err != nil {
		fmt.Println("Invalid index")
		return
	}
	if err := c.st.GetBookmarks().Remove(idx); err != nil {
		fmt.Printf("Remove: %v\n", err)
	}
}

// --- session persistence ---

func saveSession(st *app.State) {
	path := promptPath("Save file")
	if err := store.SaveState(path, st.GetBookmarks(), st.GetSession()); err != nil {
		fmt.Printf("Save failed: %v\n", err)
		return
	}
	fmt.Printf("Saved to %s\n", path)
}

func loadSession(st *app.State) {
	path := promptPath("Load file")
	loaded, err := store.LoadState(path, st.GetBookmarks())
	if err != nil {
		fmt.Printf("Load failed: %v\n", err)
		return
	}
	if loaded != nil {
		// A saved session carries no driver; rebind it to the live one.
		loaded.SetDriver(st.GetDriver())
	}
	st.SetSession(loaded)
	fmt.Printf("Loaded from %s\n", path)
}

func promptPath(label string) string {
	path := Prompt(fmt.Sprintf("%s [default: %s]: ", label, store.DefaultStateFile))
	if path == "" {
		return store.DefaultStateFile
	}
	return path
}

// --- output helpers ---

func printAddrList(addrs []uintptr, emptyMsg string) {
	if len(addrs) == 0 {
		fmt.Println(emptyMsg)
		return
	}
	for _, addr := range addrs {
		fmt.Printf("  0x%x\n", addr)
	}
}
