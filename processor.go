package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PhotoProcessor handles the photo reorganization process
type PhotoProcessor struct {
	config        *Config
	stats         *ProcessStats
	sshClient     *SSHClient
	destSSHClient *SSHClient
	startTime     time.Time
	lastProgress  time.Time
}

// ProcessStats tracks statistics during processing
type ProcessStats struct {
	TotalFiles      int
	ProcessedFiles  int
	SkippedFiles    int
	ErrorFiles      int
	MovedFiles      int
	UpdatedMetadata int
}

// NewPhotoProcessor creates a new photo processor
func NewPhotoProcessor(config *Config) *PhotoProcessor {
	return &PhotoProcessor{
		config: config,
		stats:  &ProcessStats{},
	}
}

// Process runs the photo reorganization process
func (p *PhotoProcessor) Process() error {
	p.startTime = time.Now()
	p.lastProgress = time.Now()

	// Check if exiftool is available
	if !checkExiftoolAvailable() {
		log.Println("Warning: exiftool not found. EXIF metadata will not be updated.")
		log.Println("Install exiftool: https://exiftool.org/")
	}

	// Initialize SSH client for source if needed
	if p.config.SSHHost != "" {
		client, err := NewSSHClient(p.config.SSHHost)
		if err != nil {
			return fmt.Errorf("failed to create SSH client for source: %w", err)
		}
		p.sshClient = client
		defer p.sshClient.Close()
	}

	// Initialize SSH client for destination if needed
	if p.config.RemoteDest {
		if p.config.DestSSHHost == "" {
			return fmt.Errorf("remote destination requires -dest-ssh-host or -ssh-host")
		}

		// If dest and source are on same host, reuse the connection
		if p.config.DestSSHHost == p.config.SSHHost && p.sshClient != nil {
			p.destSSHClient = p.sshClient
		} else {
			client, err := NewSSHClient(p.config.DestSSHHost)
			if err != nil {
				return fmt.Errorf("failed to create SSH client for destination: %w", err)
			}
			p.destSSHClient = client
			defer p.destSSHClient.Close()
		}
	}

	// Walk through source directory
	err := p.walkDirectory(p.config.SourceDir)
	if err != nil {
		return fmt.Errorf("failed to process directory: %w", err)
	}

	// Print statistics
	p.printStats()

	return nil
}

// walkDirectory recursively walks through directories and processes photos
func (p *PhotoProcessor) walkDirectory(dir string) error {
	if p.sshClient != nil {
		return p.walkRemoteDirectory(dir)
	}
	return p.walkLocalDirectory(dir)
}

// walkLocalDirectory walks through local directories
func (p *PhotoProcessor) walkLocalDirectory(dir string) error {
	// First pass: count total files
	imageFiles := []string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			log.Printf("Error accessing %s: %v", path, err)
			return nil
		}

		// Skip directories and non-photo files
		if info.IsDir() {
			// Skip @eaDir directories (Synology metadata)
			if strings.Contains(path, "@eaDir") {
				return filepath.SkipDir
			}
			return nil
		}

		// Process only image files
		if !isImageFile(path) {
			return nil
		}

		imageFiles = append(imageFiles, path)
		return nil
	})

	if err != nil {
		return err
	}

	p.stats.TotalFiles = len(imageFiles)
	log.Printf("Found %d image files to process", p.stats.TotalFiles)

	// Second pass: process files
	for _, path := range imageFiles {
		if err := p.processPhoto(path); err != nil {
			log.Printf("Error processing %s: %v", path, err)
			p.stats.ErrorFiles++
		} else {
			p.stats.ProcessedFiles++
		}

		// Print progress every 100 files or every 10 seconds
		p.printProgress(false)
	}

	return nil
}

// walkRemoteDirectory walks through remote SSH directories
func (p *PhotoProcessor) walkRemoteDirectory(dir string) error {
	files, err := p.sshClient.WalkDirectory(dir)
	if err != nil {
		return err
	}

	// First pass: count total files
	imageFiles := []string{}
	for _, path := range files {
		// Skip @eaDir directories
		if strings.Contains(path, "@eaDir") {
			continue
		}

		// Process only image files
		if !isImageFile(path) {
			continue
		}

		imageFiles = append(imageFiles, path)
	}

	p.stats.TotalFiles = len(imageFiles)
	log.Printf("Found %d image files to process", p.stats.TotalFiles)

	// Second pass: process files
	for _, path := range imageFiles {
		if err := p.processRemotePhoto(path); err != nil {
			log.Printf("Error processing %s: %v", path, err)
			p.stats.ErrorFiles++
		} else {
			p.stats.ProcessedFiles++
		}

		// Print progress every 100 files or every 10 seconds
		p.printProgress(false)
	}

	return nil
}

// processPhoto processes a single photo file
func (p *PhotoProcessor) processPhoto(filePath string) error {
	if p.config.Verbose {
		log.Printf("Processing: %s", filePath)
	}

	// Parse date from filename
	dateInfo, err := ParseDateFromFilename(filePath)
	if err != nil {
		p.stats.SkippedFiles++
		if p.config.Verbose {
			log.Printf("No date found, copying to unknown: %s", filePath)
		}

		// Copy to "unknown" folder instead of skipping
		if !p.config.DryRun {
			base := filepath.Base(filePath)
			unknownPath := filepath.Join(p.config.DestDir, "unknown", base)

			if err := os.MkdirAll(filepath.Join(p.config.DestDir, "unknown"), 0755); err != nil {
				return fmt.Errorf("failed to create unknown directory: %w", err)
			}

			if err := copyFile(filePath, unknownPath); err != nil {
				return fmt.Errorf("failed to copy to unknown: %w", err)
			}
		}
		return nil
	}

	// Extract description from filename
	base := filePath[strings.LastIndex(filePath, "/")+1:]
	ext := filePath[strings.LastIndex(filePath, "."):]
	desc := strings.TrimSuffix(base, ext)

	// Generate standardized filename
	newFilename := dateInfo.StandardizedFilename(desc, ext)
	destPath := filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)
	// Check if destination already exists (for resume capability)
	if p.config.SkipExisting {
		if _, err := os.Stat(destPath); err == nil {
			if p.config.Verbose {
				log.Printf("Skipping (already exists): %s", destPath)
			}
			p.stats.SkippedFiles++
			return nil
		}
	}
	if p.config.DryRun {
		log.Printf("[DRY RUN] Would move: %s -> %s", filePath, destPath)
		return nil
	}

	// Create destination directory
	destDir := filepath.Dir(destPath)
	if err := os.MkdirAll(destDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", destDir, err)
	}

	// Copy file
	if err := copyFile(filePath, destPath); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}
	p.stats.MovedFiles++

	// Update EXIF metadata
	if checkExiftoolAvailable() {
		if err := UpdateExifDate(destPath, dateInfo.ToTime()); err != nil {
			log.Printf("Warning: failed to update EXIF for %s: %v", destPath, err)
		} else {
			p.stats.UpdatedMetadata++
		}
	}

	return nil
}

// processRemotePhoto processes a photo from remote SSH location
func (p *PhotoProcessor) processRemotePhoto(remotePath string) error {
	if p.config.Verbose {
		log.Printf("Processing remote: %s", remotePath)
	}

	// Parse date from filename
	dateInfo, err := ParseDateFromFilename(remotePath)
	if err != nil {
		p.stats.SkippedFiles++
		if p.config.Verbose {
			log.Printf("No date found, copying to unknown: %s", remotePath)
		}

		// Copy to "unknown" folder instead of skipping
		if !p.config.DryRun {
			base := remotePath[strings.LastIndex(remotePath, "/")+1:]
			var unknownPath string
			if p.config.RemoteDest {
				unknownPath = filepath.Join(p.config.DestDir, "unknown", base)
			} else {
				unknownPath = filepath.Join(p.config.DestDir, "unknown", base)
			}

			// Download to temporary file
			tempFile, err := os.CreateTemp("", "photo-*"+filepath.Ext(remotePath))
			if err != nil {
				return fmt.Errorf("failed to create temp file: %w", err)
			}
			tempPath := tempFile.Name()
			tempFile.Close()
			defer os.Remove(tempPath)

			if err := p.sshClient.DownloadFile(remotePath, tempPath); err != nil {
				return fmt.Errorf("failed to download file: %w", err)
			}

			// Upload or copy to unknown folder
			if p.config.RemoteDest {
				if err := p.destSSHClient.CreateDirectory(filepath.Join(p.config.DestDir, "unknown")); err != nil {
					return fmt.Errorf("failed to create unknown directory: %w", err)
				}
				if err := p.destSSHClient.UploadFile(tempPath, unknownPath); err != nil {
					return fmt.Errorf("failed to upload to unknown: %w", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(p.config.DestDir, "unknown"), 0755); err != nil {
					return fmt.Errorf("failed to create unknown directory: %w", err)
				}
				if err := copyFile(tempPath, unknownPath); err != nil {
					return fmt.Errorf("failed to copy to unknown: %w", err)
				}
			}
		}
		return nil
	}

	// Extract description from filename
	base := remotePath[strings.LastIndex(remotePath, "/")+1:]
	ext := remotePath[strings.LastIndex(remotePath, "."):]
	desc := strings.TrimSuffix(base, ext)

	// Generate standardized filename
	newFilename := dateInfo.StandardizedFilename(desc, ext)

	var destPath string
	if p.config.RemoteDest {
		destPath = filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)
	} else {
		destPath = filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)
	}

	// Check if destination already exists (for resume capability)
	if p.config.SkipExisting {
		var exists bool
		var err error

		if p.config.RemoteDest {
			exists, err = p.destSSHClient.FileExists(destPath)
			if err != nil {
				log.Printf("Warning: failed to check if file exists at %s: %v", destPath, err)
			}
		} else {
			_, err := os.Stat(destPath)
			exists = err == nil
		}

		if exists {
			if p.config.Verbose {
				log.Printf("Skipping (already exists): %s", destPath)
			}
			p.stats.SkippedFiles++
			return nil
		}
	}

	if p.config.DryRun {
		if p.config.RemoteDest {
			log.Printf("[DRY RUN] Would process remote to remote: %s -> %s", remotePath, destPath)
		} else {
			log.Printf("[DRY RUN] Would download and move: %s -> %s", remotePath, destPath)
		}
		return nil
	}

	// Download to temporary file
	tempFile, err := os.CreateTemp("", "photo-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	if err := p.sshClient.DownloadFile(remotePath, tempPath); err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}

	// Update EXIF metadata
	if checkExiftoolAvailable() {
		if err := UpdateExifDate(tempPath, dateInfo.ToTime()); err != nil {
			log.Printf("Warning: failed to update EXIF for %s: %v", tempPath, err)
		} else {
			p.stats.UpdatedMetadata++
		}
	}

	// Upload to destination (remote or local)
	if p.config.RemoteDest {
		// Create destination directory on remote
		destDir := filepath.Dir(destPath)
		if err := p.destSSHClient.CreateDirectory(destDir); err != nil {
			return fmt.Errorf("failed to create remote directory %s: %w", destDir, err)
		}

		// Upload file to remote destination
		if err := p.destSSHClient.UploadFile(tempPath, destPath); err != nil {
			return fmt.Errorf("failed to upload file: %w", err)
		}
	} else {
		// Copy to local destination
		destDir := filepath.Dir(destPath)
		if err := os.MkdirAll(destDir, 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", destDir, err)
		}

		if err := copyFile(tempPath, destPath); err != nil {
			return fmt.Errorf("failed to copy file: %w", err)
		}
	}

	p.stats.MovedFiles++

	return nil
}

// isImageFile checks if a file is an image based on extension
func isImageFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".tif", ".heic", ".heif"}
	for _, imgExt := range imageExts {
		if ext == imgExt {
			return true
		}
	}
	return false
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	// Sync to ensure write is complete
	return destFile.Sync()
}

// printProgress prints progress updates periodically
func (p *PhotoProcessor) printProgress(force bool) {
	now := time.Now()
	timeSinceLastProgress := now.Sub(p.lastProgress)
	processed := p.stats.ProcessedFiles + p.stats.SkippedFiles + p.stats.ErrorFiles

	// Print every 100 files or every 10 seconds, whichever comes first
	if !force && processed%100 != 0 && timeSinceLastProgress < 10*time.Second {
		return
	}

	p.lastProgress = now
	elapsed := now.Sub(p.startTime)

	if processed == 0 {
		return
	}

	// Calculate rate and ETA
	rate := float64(processed) / elapsed.Seconds()
	var eta string
	if rate > 0 && p.stats.TotalFiles > processed {
		remaining := p.stats.TotalFiles - processed
		etaSeconds := float64(remaining) / rate
		etaDuration := time.Duration(etaSeconds) * time.Second
		eta = fmt.Sprintf(" | ETA: %s", formatDuration(etaDuration))
	} else {
		eta = ""
	}

	log.Printf("Progress: %d/%d files (%.1f%%) | Processed: %d | Skipped: %d | Errors: %d | Rate: %.1f files/sec | Elapsed: %s%s",
		processed, p.stats.TotalFiles,
		float64(processed)/float64(p.stats.TotalFiles)*100,
		p.stats.ProcessedFiles, p.stats.SkippedFiles, p.stats.ErrorFiles,
		rate, formatDuration(elapsed), eta)
}

// formatDuration formats a duration in a human-readable way
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second

	if h > 0 {
		return fmt.Sprintf("%dh%dm%ds", h, m, s)
	} else if m > 0 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// printStats prints processing statistics
func (p *PhotoProcessor) printStats() {
	fmt.Println("\n=== Processing Statistics ===")
	fmt.Printf("Total files found:      %d\n", p.stats.TotalFiles)
	fmt.Printf("Successfully processed: %d\n", p.stats.ProcessedFiles)
	fmt.Printf("Skipped (no date):      %d\n", p.stats.SkippedFiles)
	fmt.Printf("Errors:                 %d\n", p.stats.ErrorFiles)
	fmt.Printf("Files moved:            %d\n", p.stats.MovedFiles)
	fmt.Printf("Metadata updated:       %d\n", p.stats.UpdatedMetadata)
	fmt.Println("============================")
}
