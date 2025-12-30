package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rwcarlsen/goexif/exif"
	"github.com/rwcarlsen/goexif/mknote"
)

func init() {
	// Register maker note handlers
	exif.RegisterParsers(mknote.All...)
}

// ExifMetadata represents EXIF data for a photo
type ExifMetadata struct {
	DateTimeOriginal time.Time
	Make             string
	Model            string
	Width            int
	Height           int
}

// ReadExifData reads EXIF metadata from a photo file
func ReadExifData(filepath string) (*ExifMetadata, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err != nil {
		// Many photos might not have EXIF data, which is okay
		return &ExifMetadata{}, nil
	}

	metadata := &ExifMetadata{}

	// Try to get DateTimeOriginal
	if tm, err := x.DateTime(); err == nil {
		metadata.DateTimeOriginal = tm
	}

	// Try to get camera make
	if make, err := x.Get(exif.Make); err == nil {
		if val, err := make.StringVal(); err == nil {
			metadata.Make = val
		}
	}

	// Try to get camera model
	if model, err := x.Get(exif.Model); err == nil {
		if val, err := model.StringVal(); err == nil {
			metadata.Model = val
		}
	}

	// Try to get image dimensions
	if width, err := x.Get(exif.PixelXDimension); err == nil {
		if val, err := width.Int(0); err == nil {
			metadata.Width = val
		}
	}

	if height, err := x.Get(exif.PixelYDimension); err == nil {
		if val, err := height.Int(0); err == nil {
			metadata.Height = val
		}
	}

	return metadata, nil
}

// UpdateExifDate updates the EXIF DateTimeOriginal field in a photo
// Note: This is a placeholder. Updating EXIF data is complex and typically
// requires external tools like exiftool
func UpdateExifDate(filepath string, date time.Time) error {
	// For now, we'll use exiftool as it's the most reliable way
	// The actual implementation will shell out to exiftool
	return updateExifWithExiftool(filepath, date)
}

// DetermineCorrectTimestamp decides which timestamp to use:
// - If original EXIF has a timestamp and its year matches the parsed year, use original EXIF
// - Otherwise, use the parsed date
func DetermineCorrectTimestamp(sourcePath string, parsedDate *DateInfo) time.Time {
	// Read original EXIF
	exifData, err := ReadExifData(sourcePath)
	if err != nil || exifData.DateTimeOriginal.IsZero() {
		// No EXIF data, use parsed date
		return parsedDate.ToTime()
	}

	// Check if years match
	if exifData.DateTimeOriginal.Year() == parsedDate.Year {
		// Years match, use the full original EXIF timestamp (preserves time-of-day)
		return exifData.DateTimeOriginal
	}

	// Years don't match, trust the filename/path parsing
	return parsedDate.ToTime()
}
