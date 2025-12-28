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
	sshHost := flag.String("ssh-host", "", "SSH host (e.g., nas-photos or user@host:port)")
	verbose := flag.Bool("verbose", false, "Enable verbose logging")

	flag.Parse()

	if *sourceDir == "" || *destDir == "" {
		fmt.Println("Usage: picture-metadata -source <source-dir> -dest <dest-dir> [options]")
		flag.PrintDefaults()
		os.Exit(1)
	}

	config := &Config{
		SourceDir: *sourceDir,
		DestDir:   *destDir,
		DryRun:    *dryRun,
		SSHHost:   *sshHost,
		Verbose:   *verbose,
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
