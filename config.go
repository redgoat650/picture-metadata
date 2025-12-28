package main

// Config holds the application configuration
type Config struct {
	SourceDir string
	DestDir   string
	DryRun    bool
	SSHHost   string
	Verbose   bool
}
