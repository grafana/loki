package sortmerge

// SetSectionHooksForTest installs observers invoked when a windowed-merge section
// reader is opened and closed, letting tests measure peak concurrent readers.
// Test-only; production leaves the hooks nil.
func SetSectionHooksForTest(open, closeFn func()) {
	onSectionOpen = open
	onSectionClose = closeFn
}
