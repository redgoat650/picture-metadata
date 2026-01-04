package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var useDockerExiftool = false

// updateExifWithExiftool uses the exiftool command to update EXIF metadata
func updateExifWithExiftool(filePath string, date time.Time) error {
	// Check if we should use Docker
	if useDockerExiftool {
		return updateExifWithDocker(filePath, date)
	}

	// Check if exiftool is available natively
	if _, err := exec.LookPath("exiftool"); err != nil {
		return fmt.Errorf("exiftool not found in PATH. Please install it: %w", err)
	}

	// Format date for EXIF (YYYY:MM:DD HH:MM:SS)
	dateStr := date.Format("2006:01:02 15:04:05")

	// Update multiple date/time fields to ensure consistency
	fields := []string{
		"DateTimeOriginal",
		"CreateDate",
		"ModifyDate",
	}

	for _, field := range fields {
		cmd := exec.Command("exiftool",
			"-overwrite_original",
			fmt.Sprintf("-%s=%s", field, dateStr),
			filePath,
		)

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to update %s: %w", field, err)
		}
	}

	return nil
}

// updateExifWithDocker uses Docker to run exiftool
func updateExifWithDocker(filePath string, date time.Time) error {
	// Format date for EXIF (YYYY:MM:DD HH:MM:SS)
	dateStr := date.Format("2006:01:02 15:04:05")

	// Get absolute path and directory
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	dir := filepath.Dir(absPath)
	filename := filepath.Base(absPath)

	// Update multiple date/time fields to ensure consistency
	fields := []string{
		"DateTimeOriginal",
		"CreateDate",
		"ModifyDate",
	}

	for _, field := range fields {
		cmd := exec.Command("docker", "run", "--rm",
			"-v", fmt.Sprintf("%s:/work", dir),
			"exiftool/exiftool",
			"-overwrite_original",
			fmt.Sprintf("-%s=%s", field, dateStr),
			fmt.Sprintf("/work/%s", filename),
		)

		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to update %s with Docker: %w", field, err)
		}
	}

	return nil
}

// checkExiftoolAvailable checks if exiftool is installed (native or Docker)
func checkExiftoolAvailable() bool {
	// First check for native exiftool
	if _, err := exec.LookPath("exiftool"); err == nil {
		return true
	}

	// Check if Docker is available
	if _, err := exec.LookPath("docker"); err == nil {
		// Test if we can use the exiftool Docker image
		cmd := exec.Command("docker", "image", "inspect", "exiftool/exiftool")
		if cmd.Run() == nil {
			useDockerExiftool = true
			return true
		}

		// Try to pull the image
		fmt.Println("Pulling exiftool Docker image (this may take a moment)...")
		cmd = exec.Command("docker", "pull", "exiftool/exiftool")
		if cmd.Run() == nil {
			useDockerExiftool = true
			return true
		}
	}

	return false
}

// ReadTimestampWithExiftool reads timestamp from any media file (image or video) using exiftool
// Returns the timestamp and true if found, or zero time and false if not found
func ReadTimestampWithExiftool(filePath string) (time.Time, bool) {
	// Check if exiftool is available
	if _, err := exec.LookPath("exiftool"); err != nil {
		return time.Time{}, false
	}

	// Use exiftool to read DateTimeOriginal, CreateDate, or MediaCreateDate
	// Try DateTimeOriginal first (standard for photos)
	cmd := exec.Command("exiftool", "-DateTimeOriginal", "-CreateDate", "-MediaCreateDate", "-s", "-s", "-s", filePath)
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return time.Time{}, false
	}

	// Parse the first non-empty timestamp
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Try multiple timestamp formats
		formats := []string{
			"2006:01:02 15:04:05",
			"2006:01:02 15:04:05-07:00",
			"2006:01:02 15:04:05Z",
			"2006-01-02T15:04:05",
		}

		for _, format := range formats {
			if t, err := time.Parse(format, line); err == nil {
				return t, true
			}
		}
	}

	return time.Time{}, false
}
