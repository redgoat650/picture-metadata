package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// PhotoProcessor handles the photo reorganization process
type PhotoProcessor struct {
	config               *Config
	stats                *ProcessStats
	sshClient            *SSHClient
	destSSHClient        *SSHClient
	startTime            time.Time
	lastProgress         time.Time
	statsMutex           sync.Mutex
	timestampMap         map[string]time.Time // Tracks last timestamp used for each date (YYYY-MM-DD)
	timestampMutex       sync.Mutex           // Protects timestampMap for concurrent access
	timestampAssignments map[string]time.Time // Pre-allocated timestamps for each file path
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
		config:               config,
		stats:                &ProcessStats{},
		timestampMap:         make(map[string]time.Time),
		timestampAssignments: make(map[string]time.Time),
	}
}

// naturalSort sorts strings using natural/alphanumeric ordering
// where numbers are compared numerically rather than lexicographically
// Example: file1, file2, file10, file20 (not file1, file10, file2, file20)
func naturalSort(paths []string) {
	sort.Slice(paths, func(i, j int) bool {
		return naturalLess(paths[i], paths[j])
	})
}

// naturalLess compares two strings using natural ordering
func naturalLess(a, b string) bool {
	// Regular expression to split on numeric sequences
	re := regexp.MustCompile(`(\d+|\D+)`)
	aParts := re.FindAllString(a, -1)
	bParts := re.FindAllString(b, -1)

	for idx := 0; idx < len(aParts) && idx < len(bParts); idx++ {
		aPart := aParts[idx]
		bPart := bParts[idx]

		// Check if both parts are numeric
		aNum, aIsNum := strconv.Atoi(aPart)
		bNum, bIsNum := strconv.Atoi(bPart)

		if aIsNum == nil && bIsNum == nil {
			// Both are numbers - compare numerically
			if aNum != bNum {
				return aNum < bNum
			}
		} else {
			// At least one is not a number - compare lexicographically
			if aPart != bPart {
				return aPart < bPart
			}
		}
	}

	// If all parts match, shorter string comes first
	return len(aParts) < len(bParts)
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

	// Determine the directory to process
	processDir := p.config.SourceDir
	if p.config.TestDir != "" {
		// TestDir is relative to SourceDir
		processDir = filepath.Join(p.config.SourceDir, p.config.TestDir)
		log.Printf("Processing test directory: %s", processDir)
	}

	// Walk through source directory
	err := p.walkDirectory(processDir)
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

		// Process only media files (images and videos)
		if !isMediaFile(path) {
			return nil
		}

		imageFiles = append(imageFiles, path)
		return nil
	})

	if err != nil {
		return err
	}

	p.stats.TotalFiles = len(imageFiles)
	log.Printf("Found %d media files to process with %d workers", p.stats.TotalFiles, p.config.Workers)

	// Sort files using natural sort to ensure correct numeric ordering
	// (e.g., file1, file2, file10 instead of file1, file10, file2)
	naturalSort(imageFiles)

	// Pre-allocate timestamps for files to ensure correct ordering
	p.preallocateTimestamps(imageFiles)

	// Second pass: process files with worker pool
	jobs := make(chan string, len(imageFiles))
	results := make(chan error, len(imageFiles))
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < p.config.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				err := p.processPhoto(path)
				results <- err
			}
		}()
	}

	// Send jobs
	go func() {
		for _, path := range imageFiles {
			jobs <- path
		}
		close(jobs)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results and update stats
	for err := range results {
		if err != nil {
			p.statsMutex.Lock()
			p.stats.ErrorFiles++
			p.statsMutex.Unlock()
		}
		// Note: ProcessedFiles and SkippedFiles are incremented within
		// the processing functions themselves, not here

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

		// Process only media files (images and videos)
		if !isMediaFile(path) {
			continue
		}

		imageFiles = append(imageFiles, path)
	}

	p.stats.TotalFiles = len(imageFiles)
	log.Printf("Found %d media files to process with %d workers", p.stats.TotalFiles, p.config.Workers)

	// Sort files using natural sort to ensure correct numeric ordering
	// (e.g., file1, file2, file10 instead of file1, file10, file2)
	naturalSort(imageFiles)

	// Pre-allocate timestamps for files to ensure correct ordering
	p.preallocateTimestamps(imageFiles)

	// Second pass: process files with worker pool
	jobs := make(chan string, len(imageFiles))
	results := make(chan error, len(imageFiles))
	var wg sync.WaitGroup

	// Start workers
	for w := 0; w < p.config.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range jobs {
				err := p.processRemotePhoto(path)
				results <- err
			}
		}()
	}

	// Send jobs
	go func() {
		for _, path := range imageFiles {
			jobs <- path
		}
		close(jobs)
	}()

	// Collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results and update stats
	for err := range results {
		if err != nil {
			p.statsMutex.Lock()
			p.stats.ErrorFiles++
			p.statsMutex.Unlock()
		}
		// Note: ProcessedFiles and SkippedFiles are incremented within
		// the processing functions themselves, not here

		// Print progress every 100 files or every 10 seconds
		p.printProgress(false)
	}

	return nil
}

// preallocateTimestamps pre-allocates timestamps for all files in natural sort order
// to ensure that concurrent processing maintains correct ordering.
// Files with real EXIF keep their timestamps, files without EXIF get sequential
// timestamps that maintain the natural sort order.
func (p *PhotoProcessor) preallocateTimestamps(filePaths []string) {
	var lastTimestamp time.Time

	for _, filePath := range filePaths {
		// Parse date from filename
		dateInfo, err := ParseDateFromFilename(filePath)
		if err != nil {
			// Skip files without dates
			continue
		}

		var correctTimestamp time.Time
		var isFromEXIF bool

		// Check if source file has real EXIF that matches the parsed year
		// For remote files, we need to download temporarily
		if p.sshClient != nil {
			// Remote file - download temporarily to check EXIF
			ext := filepath.Ext(filePath)
			tempFile, err := os.CreateTemp("", "prealloc-*"+ext)
			if err == nil {
				tempPath := tempFile.Name()
				tempFile.Close()
				defer os.Remove(tempPath)

				if err := p.sshClient.DownloadFile(filePath, tempPath); err == nil {
					correctTimestamp, isFromEXIF = DetermineCorrectTimestamp(tempPath, dateInfo)
				} else {
					// Download failed, use parsed date
					correctTimestamp = dateInfo.ToTime()
					isFromEXIF = false
				}
			} else {
				// Temp file creation failed, use parsed date
				correctTimestamp = dateInfo.ToTime()
				isFromEXIF = false
			}
		} else {
			// Local file
			correctTimestamp, isFromEXIF = DetermineCorrectTimestamp(filePath, dateInfo)
		}

		var finalTimestamp time.Time
		if isFromEXIF {
			// Real EXIF data is sacred - always use it as-is
			finalTimestamp = correctTimestamp
			// Update lastTimestamp to maintain sequential order for subsequent non-EXIF files
			// Only if this EXIF timestamp is later than what we've seen
			if correctTimestamp.After(lastTimestamp) {
				lastTimestamp = correctTimestamp
			}
		} else {
			// No matching EXIF - allocate sequential timestamp in natural filename order
			// Start at midnight (00:00:00) so real EXIF timestamps (usually daytime) sort after
			if lastTimestamp.IsZero() {
				// First file without EXIF - start at midnight of the parsed date
				baseDate := dateInfo.ToTime()
				finalTimestamp = time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), 0, 0, 0, 0, baseDate.Location())
			} else {
				// Subsequent files without EXIF - continue from last timestamp (whether it was EXIF or sequential)
				finalTimestamp = lastTimestamp.Add(1 * time.Second)
			}
			lastTimestamp = finalTimestamp
		}

		// Store the pre-allocated timestamp
		p.timestampAssignments[filePath] = finalTimestamp

		if p.config.Verbose {
			source := "EXIF"
			if !isFromEXIF {
				source = "parsed+sequential"
			}
			log.Printf("[Prealloc] %s -> %s (from %s)", filepath.Base(filePath), finalTimestamp.Format("2006-01-02 15:04:05"), source)
		}
	}
}

// GetSequentialTimestamp retrieves the pre-allocated timestamp for a file path
// to preserve natural sort ordering in photo apps like iCloud Photos.
func (p *PhotoProcessor) GetSequentialTimestamp(filePath string, baseTimestamp time.Time) time.Time {
	// Check if we have a pre-allocated timestamp
	if timestamp, exists := p.timestampAssignments[filePath]; exists {
		return timestamp
	}

	// Fallback: use base timestamp (shouldn't happen in normal operation)
	log.Printf("Warning: No pre-allocated timestamp for %s, using base timestamp", filePath)
	return baseTimestamp
}

// processPhoto processes a single photo file
func (p *PhotoProcessor) processPhoto(filePath string) error {
	if p.config.Verbose {
		log.Printf("Processing: %s", filePath)
	}

	// Parse date from filename
	dateInfo, err := ParseDateFromFilename(filePath)
	if err != nil {
		log.Printf("Skipping (no date found): %s -> unknown/", filePath)

		// Copy to "unknown" folder instead of skipping
		if !p.config.DryRun {
			base := filepath.Base(filePath)
			unknownPath := filepath.Join(p.config.DestDir, "unknown", base)

			if err := os.MkdirAll(filepath.Join(p.config.DestDir, "unknown"), 0755); err != nil {
				return fmt.Errorf("failed to create unknown directory: %w", err)
			}

			// Handle duplicate filenames by appending a counter
			finalPath := unknownPath
			counter := 1
			ext := filepath.Ext(base)
			nameWithoutExt := strings.TrimSuffix(base, ext)
			for {
				if _, err := os.Stat(finalPath); os.IsNotExist(err) {
					break
				}
				finalPath = filepath.Join(p.config.DestDir, "unknown", fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext))
				counter++
			}

			if err := copyFile(filePath, finalPath); err != nil {
				log.Printf("ERROR: Failed to copy to unknown: %s - %v", filePath, err)
				return fmt.Errorf("failed to copy to unknown: %w", err)
			}
		}
		p.stats.SkippedFiles++
		return nil
	}

	// Extract description from filename
	base := filePath[strings.LastIndex(filePath, "/")+1:]
	ext := filePath[strings.LastIndex(filePath, "."):]
	desc := strings.TrimSuffix(base, ext)

	// Extract directory context and prepend to description
	dirContext := ExtractDirectoryContext(filePath, p.config.SourceDir)
	if dirContext != "" {
		desc = dirContext + "_" + desc
	}

	// Generate standardized filename
	newFilename := dateInfo.StandardizedFilename(desc, ext)
	destPath := filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)

	// In fix-metadata mode, we only update EXIF, no copying
	if p.config.FixMetadata {
		// Check if destination file exists
		if _, err := os.Stat(destPath); os.IsNotExist(err) {
			if p.config.Verbose {
				log.Printf("Skipping (dest doesn't exist): %s", destPath)
			}
			p.stats.SkippedFiles++
			return nil
		}

		// Determine correct timestamp (original EXIF if year matches, otherwise parsed)
		correctTimestamp, isFromEXIF := DetermineCorrectTimestamp(filePath, dateInfo)

		// If not from EXIF (i.e., parsed date), use sequential timestamps to preserve lexicographic order
		if !isFromEXIF {
			correctTimestamp = p.GetSequentialTimestamp(filePath, correctTimestamp)
		}

		if p.config.DryRun {
			source := "EXIF"
			if !isFromEXIF {
				source = "parsed+sequential"
			}
			log.Printf("[DRY RUN] Would fix metadata: %s -> %s (from %s)", destPath, correctTimestamp.Format("2006-01-02 15:04:05"), source)
			return nil
		}

		// Update EXIF/metadata for both images and videos
		if checkExiftoolAvailable() {
			if err := UpdateExifDate(destPath, correctTimestamp); err != nil {
				log.Printf("Warning: failed to update metadata for %s: %v", destPath, err)
			} else {
				p.stats.UpdatedMetadata++
			}
		}

		p.stats.ProcessedFiles++
		return nil
	}

	// Normal mode: copy file and update EXIF
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

	// Determine correct timestamp for EXIF
	// Check if source has real EXIF that matches the parsed year
	correctTimestamp, isFromEXIF := DetermineCorrectTimestamp(filePath, dateInfo)

	// If not from EXIF (i.e., parsed date), use sequential timestamps to preserve lexicographic order
	var timestamp time.Time
	if !isFromEXIF {
		timestamp = p.GetSequentialTimestamp(filePath, correctTimestamp)
	} else {
		timestamp = correctTimestamp
	}

	if p.config.DryRun {
		source := "EXIF"
		if !isFromEXIF {
			source = "parsed+sequential"
		}
		log.Printf("[DRY RUN] Would move: %s -> %s | timestamp: %s (from %s)", filePath, destPath, timestamp.Format("2006-01-02 15:04:05"), source)
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

	// Update EXIF/metadata for both images and videos
	if checkExiftoolAvailable() {
		if err := UpdateExifDate(destPath, timestamp); err != nil {
			log.Printf("Warning: failed to update metadata for %s: %v", destPath, err)
		} else {
			p.stats.UpdatedMetadata++
		}
	}

	p.stats.ProcessedFiles++
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
		log.Printf("Skipping (no date found): %s -> unknown/", remotePath)

		// Copy to "unknown" folder instead of skipping
		if !p.config.DryRun {
			base := remotePath[strings.LastIndex(remotePath, "/")+1:]
			unknownPath := filepath.Join(p.config.DestDir, "unknown", base)

			// Download to temporary file
			tempFile, err := os.CreateTemp("", "photo-*"+filepath.Ext(remotePath))
			if err != nil {
				return fmt.Errorf("failed to create temp file: %w", err)
			}
			tempPath := tempFile.Name()
			tempFile.Close()
			defer os.Remove(tempPath)

			if err := p.sshClient.DownloadFile(remotePath, tempPath); err != nil {
				log.Printf("ERROR: Failed to download file: %s - %v", remotePath, err)
				return fmt.Errorf("failed to download file: %w", err)
			}

			// Handle duplicate filenames by appending a counter
			finalPath := unknownPath
			counter := 1
			ext := filepath.Ext(base)
			nameWithoutExt := strings.TrimSuffix(base, ext)

			// Upload or copy to unknown folder
			if p.config.RemoteDest {
				if err := p.destSSHClient.CreateDirectory(filepath.Join(p.config.DestDir, "unknown")); err != nil {
					return fmt.Errorf("failed to create unknown directory: %w", err)
				}

				// Check for duplicates and find available filename
				for {
					exists, err := p.destSSHClient.FileExists(finalPath)
					if err != nil {
						return fmt.Errorf("failed to check if file exists: %w", err)
					}
					if !exists {
						break
					}
					finalPath = filepath.Join(p.config.DestDir, "unknown", fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext))
					counter++
				}

				if err := p.destSSHClient.UploadFile(tempPath, finalPath); err != nil {
					log.Printf("ERROR: Failed to upload to unknown: %s - %v", finalPath, err)
					return fmt.Errorf("failed to upload to unknown: %w", err)
				}
			} else {
				if err := os.MkdirAll(filepath.Join(p.config.DestDir, "unknown"), 0755); err != nil {
					return fmt.Errorf("failed to create unknown directory: %w", err)
				}

				// Check for duplicates and find available filename
				for {
					if _, err := os.Stat(finalPath); os.IsNotExist(err) {
						break
					}
					finalPath = filepath.Join(p.config.DestDir, "unknown", fmt.Sprintf("%s_%d%s", nameWithoutExt, counter, ext))
					counter++
				}

				if err := copyFile(tempPath, finalPath); err != nil {
					log.Printf("ERROR: Failed to copy to unknown: %s - %v", finalPath, err)
					return fmt.Errorf("failed to copy to unknown: %w", err)
				}
			}
		}
		p.stats.SkippedFiles++
		return nil
	}

	// Extract description from filename
	base := remotePath[strings.LastIndex(remotePath, "/")+1:]
	ext := remotePath[strings.LastIndex(remotePath, "."):]
	desc := strings.TrimSuffix(base, ext)

	// Extract directory context and prepend to description
	dirContext := ExtractDirectoryContext(remotePath, p.config.SourceDir)
	if dirContext != "" {
		desc = dirContext + "_" + desc
	}

	// Generate standardized filename
	newFilename := dateInfo.StandardizedFilename(desc, ext)

	var destPath string
	if p.config.RemoteDest {
		destPath = filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)
	} else {
		destPath = filepath.Join(p.config.DestDir, dateInfo.GetDirectoryPath(), newFilename)
	}

	// In fix-metadata mode, we only update EXIF, no copying
	if p.config.FixMetadata {
		// Check if destination file exists
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

		if !exists {
			if p.config.Verbose {
				log.Printf("Skipping (dest doesn't exist): %s", destPath)
			}
			p.stats.SkippedFiles++
			return nil
		}

		// Download source file temporarily to read EXIF (need this even for dry-run to determine timestamp)
		sourceTempFile, err := os.CreateTemp("", "photo-source-*"+ext)
		if err != nil {
			return fmt.Errorf("failed to create temp file for source: %w", err)
		}
		sourceTempPath := sourceTempFile.Name()
		sourceTempFile.Close()
		defer os.Remove(sourceTempPath)

		if err := p.sshClient.DownloadFile(remotePath, sourceTempPath); err != nil {
			return fmt.Errorf("failed to download source file: %w", err)
		}

		// Determine correct timestamp (original EXIF if year matches, otherwise parsed)
		correctTimestamp, isFromEXIF := DetermineCorrectTimestamp(sourceTempPath, dateInfo)

		// If not from EXIF (i.e., parsed date), use sequential timestamps to preserve lexicographic order
		if !isFromEXIF {
			correctTimestamp = p.GetSequentialTimestamp(remotePath, correctTimestamp)
		}

		if p.config.DryRun {
			source := "EXIF"
			if !isFromEXIF {
				source = "parsed+sequential"
			}
			log.Printf("[DRY RUN] Would fix metadata: %s -> %s (from %s)", filepath.Base(destPath), correctTimestamp.Format("2006-01-02 15:04:05"), source)
			return nil
		}

		if p.config.Verbose {
			source := "EXIF"
			if !isFromEXIF {
				source = "parsed+sequential"
			}
			log.Printf("[Timestamp] %s -> %s (from %s)", filepath.Base(destPath), correctTimestamp.Format("2006-01-02 15:04:05"), source)
		}

		// Update EXIF/metadata at destination for both images and videos
		if p.config.RemoteDest {
			// Download dest file, update metadata, re-upload
			destTempFile, err := os.CreateTemp("", "photo-dest-*"+ext)
			if err != nil {
				return fmt.Errorf("failed to create temp file for dest: %w", err)
			}
			destTempPath := destTempFile.Name()
			destTempFile.Close()
			defer os.Remove(destTempPath)

			if err := p.destSSHClient.DownloadFile(destPath, destTempPath); err != nil {
				return fmt.Errorf("failed to download dest file: %w", err)
			}

			if checkExiftoolAvailable() {
				if err := UpdateExifDate(destTempPath, correctTimestamp); err != nil {
					log.Printf("Warning: failed to update metadata for %s: %v", destTempPath, err)
				} else {
					// Re-upload to destination
					if err := p.destSSHClient.UploadFile(destTempPath, destPath); err != nil {
						return fmt.Errorf("failed to upload updated file: %w", err)
					}
					p.stats.UpdatedMetadata++
				}
			}
		} else {
			// Local destination, update directly
			if checkExiftoolAvailable() {
				if err := UpdateExifDate(destPath, correctTimestamp); err != nil {
					log.Printf("Warning: failed to update metadata for %s: %v", destPath, err)
				} else {
					p.stats.UpdatedMetadata++
				}
			}
		}

		p.stats.ProcessedFiles++
		return nil
	}

	// Normal mode: copy file and update EXIF
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

	// Download source file temporarily to read EXIF (need this even for dry-run to determine timestamp)
	sourceTempFile, err := os.CreateTemp("", "photo-source-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file for source: %w", err)
	}
	sourceTempPath := sourceTempFile.Name()
	sourceTempFile.Close()
	defer os.Remove(sourceTempPath)

	if err := p.sshClient.DownloadFile(remotePath, sourceTempPath); err != nil {
		return fmt.Errorf("failed to download source file: %w", err)
	}

	// Determine correct timestamp for EXIF
	// Check if source has real EXIF that matches the parsed year
	correctTimestamp, isFromEXIF := DetermineCorrectTimestamp(sourceTempPath, dateInfo)

	// If not from EXIF (i.e., parsed date), use sequential timestamps to preserve lexicographic order
	var timestamp time.Time
	if !isFromEXIF {
		timestamp = p.GetSequentialTimestamp(remotePath, correctTimestamp)
	} else {
		timestamp = correctTimestamp
	}

	if p.config.DryRun {
		source := "EXIF"
		if !isFromEXIF {
			source = "parsed+sequential"
		}
		if p.config.RemoteDest {
			log.Printf("[DRY RUN] Would process remote to remote: %s -> %s | timestamp: %s (from %s)", remotePath, destPath, timestamp.Format("2006-01-02 15:04:05"), source)
		} else {
			log.Printf("[DRY RUN] Would download and move: %s -> %s | timestamp: %s (from %s)", remotePath, destPath, timestamp.Format("2006-01-02 15:04:05"), source)
		}
		// Clean up source temp file
		os.Remove(sourceTempPath)
		return nil
	}

	// For non-dry-run, we already have the source downloaded, but we need it in a different temp file for processing
	// Move the source temp file to the processing temp file
	tempFile, err := os.CreateTemp("", "photo-*"+ext)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tempPath := tempFile.Name()
	tempFile.Close()
	defer os.Remove(tempPath)

	// Copy from source temp to processing temp
	if err := copyFile(sourceTempPath, tempPath); err != nil {
		return fmt.Errorf("failed to copy temp file: %w", err)
	}

	// Update EXIF/metadata for both images and videos
	if checkExiftoolAvailable() {
		if err := UpdateExifDate(tempPath, timestamp); err != nil {
			log.Printf("Warning: failed to update metadata for %s: %v", tempPath, err)
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

	p.stats.ProcessedFiles++
	return nil
}

// isMediaFile checks if a file is a photo or video based on extension
func isMediaFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	// Image extensions
	imageExts := []string{".jpg", ".jpeg", ".png", ".gif", ".bmp", ".tiff", ".tif", ".heic", ".heif"}
	// Video extensions
	videoExts := []string{".mp4", ".mov", ".avi", ".mkv", ".m4v", ".3gp", ".wmv", ".flv", ".webm", ".mpg", ".mpeg", ".mts", ".m2ts"}

	allExts := append(imageExts, videoExts...)
	for _, mediaExt := range allExts {
		if ext == mediaExt {
			return true
		}
	}
	return false
}

// isVideoFile checks if a file is a video based on extension
func isVideoFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	videoExts := []string{".mp4", ".mov", ".avi", ".mkv", ".m4v", ".3gp", ".wmv", ".flv", ".webm", ".mpg", ".mpeg", ".mts", ".m2ts"}
	for _, vidExt := range videoExts {
		if ext == vidExt {
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
	p.statsMutex.Lock()
	defer p.statsMutex.Unlock()

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
