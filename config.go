package main

// Config holds the application configuration
type Config struct {
	SourceDir    string
	DestDir      string
	DryRun       bool
	SSHHost      string
	DestSSHHost  string // SSH host for destination (if different from source)
	Verbose      bool
	RemoteDest   bool   // Whether destination is on remote server
	SkipExisting bool   // Skip files that already exist at destination
	Workers      int    // Number of concurrent workers
	TestDir      string // Optional: specific subdirectory under SourceDir to process
	FixMetadata  bool   // Fix metadata mode: restore original EXIF timestamps instead of copying files
}
