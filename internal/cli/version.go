package cli

// Build carries linker-injected release metadata into the command boundary.
type Build struct {
	Version string
	Commit  string
	Date    string
}
