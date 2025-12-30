package main

import (
	"flag"
	"fmt"
	"log"
	"os"
)

func main() {
	// Command-line flags
	sourceDir := flag.String("source", "", "Source directory containing photos (can be remote SSH path like user@host:path)")
	destDir := flag.String("dest", "", "Destination directory for reorganized photos")
	dryRun := flag.Bool("dry-run", false, "Perform a dry run without making changes")
	sshHost := flag.String("ssh-host", "", "SSH host for source (e.g., nas-photos or user@host:port)")
	destSSHHost := flag.String("dest-ssh-host", "", "SSH host for destination (defaults to same as source)")
	remoteDest := flag.Bool("remote-dest", false, "Whether destination is on remote server (requires -dest-ssh-host or -ssh-host)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")
	skipExisting := flag.Bool("skip-existing", false, "Skip files that already exist at destination (for resuming interrupted runs)")
	workers := flag.Int("workers", 2, "Number of concurrent workers for parallel processing")
	testDir := flag.String("test-dir", "", "Optional: specific subdirectory under -source to process (e.g., '2010-2019/2018/2018_10_21wedding official')")
	fixMetadata := flag.Bool("fix-metadata", false, "Fix metadata mode: restore original EXIF timestamps where appropriate instead of copying files")

	flag.Parse()

	if *sourceDir == "" || *destDir == "" {
		fmt.Println("Usage: picture-metadata -source <source-dir> -dest <dest-dir> [options]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	// If dest-ssh-host not specified but remote-dest is true, use same as source
	if *remoteDest && *destSSHHost == "" {
		*destSSHHost = *sshHost
	}

	config := &Config{
		SourceDir:    *sourceDir,
		DestDir:      *destDir,
		DryRun:       *dryRun,
		SSHHost:      *sshHost,
		DestSSHHost:  *destSSHHost,
		RemoteDest:   *remoteDest,
		Verbose:      *verbose,
		SkipExisting: *skipExisting,
		Workers:      *workers,
		TestDir:      *testDir,
		FixMetadata:  *fixMetadata,
	}

	if err := run(config); err != nil {
		log.Fatalf("Error: %v", err)
	}

	fmt.Println("Photo reorganization complete!")
}

func run(config *Config) error {
	processor := NewPhotoProcessor(config)
	return processor.Process()
}
