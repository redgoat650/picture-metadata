package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DateInfo represents extracted date information from a filename
type DateInfo struct {
	Year     int
	Month    int
	Day      int
	Time     string // HH:MM:SS format, if available
	Original string // Original filename
}

// ParseDateFromFilename extracts date information from photo filenames
// Supports formats like:
// - YYYYMMDD_description.jpg
// - YYYY_MM_DD_description.jpg
// - YYYY_description.jpg
// - YYMMDD_description.jpg (for years 19XX or 20XX)
// - YYMM_description.jpg (for years 19XX or 20XX, defaults to 1st of month)
func ParseDateFromFilename(filename string) (*DateInfo, error) {
	base := filepath.Base(filename)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Try various date patterns
	// Order matters! Check more specific patterns first
	patterns := []struct {
		regex   *regexp.Regexp
		extract func([]string) (*DateInfo, error)
	}{
		{
			// YYYY_MM_DD format
			regexp.MustCompile(`^(\d{4})_(\d{2})_(\d{2})`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				day, _ := strconv.Atoi(matches[3])
				return &DateInfo{Year: year, Month: month, Day: day, Original: base}, nil
			},
		},
		{
			// YYYYMMDD format (8 consecutive digits followed by non-digit or end)
			regexp.MustCompile(`^(\d{4})(\d{2})(\d{2})(?:\D|$)`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				day, _ := strconv.Atoi(matches[3])
				return &DateInfo{Year: year, Month: month, Day: day, Original: base}, nil
			},
		},
		{
			// YYMMDD format (6 consecutive digits followed by non-digit or end, assume 19XX or 20XX based on value)
			regexp.MustCompile(`^(\d{2})(\d{2})(\d{2})(?:\D|$)`),
			func(matches []string) (*DateInfo, error) {
				yy, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				day, _ := strconv.Atoi(matches[3])

				// Heuristic: if YY > 50, assume 19XX, else 20XX
				var year int
				if yy > 50 {
					year = 1900 + yy
				} else {
					year = 2000 + yy
				}

				return &DateInfo{Year: year, Month: month, Day: day, Original: base}, nil
			},
		},
		{
			// YYMM format (4 consecutive digits followed by non-digit or end, assume 19XX or 20XX based on value)
			regexp.MustCompile(`^(\d{2})(\d{2})(?:\D|$)`),
			func(matches []string) (*DateInfo, error) {
				yy, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])

				// Heuristic: if YY > 50, assume 19XX, else 20XX
				var year int
				if yy > 50 {
					year = 1900 + yy
				} else {
					year = 2000 + yy
				}

				// Default to 1st of the month
				return &DateInfo{Year: year, Month: month, Day: 1, Original: base}, nil
			},
		},
		{
			// YYYY only format (year only, no specific month/day)
			regexp.MustCompile(`^(\d{4})_`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				// Default to January 1st when only year is available
				return &DateInfo{Year: year, Month: 1, Day: 1, Original: base}, nil
			},
		},
	}

	for _, pattern := range patterns {
		if matches := pattern.regex.FindStringSubmatch(name); matches != nil {
			info, err := pattern.extract(matches)
			if err != nil {
				continue
			}

			// Validate year range (reasonable for photos)
			if info.Year < 1800 || info.Year > 2100 {
				continue
			}

			// Validate date
			if info.Month < 1 || info.Month > 12 {
				continue
			}
			if info.Day < 1 || info.Day > 31 {
				continue
			}

			return info, nil
		}
	}

	return nil, fmt.Errorf("could not parse date from filename: %s", filename)
}

// ToTime converts DateInfo to time.Time
func (d *DateInfo) ToTime() time.Time {
	if d.Time != "" {
		// Parse HH:MM:SS if available
		parts := strings.Split(d.Time, ":")
		if len(parts) == 3 {
			hour, _ := strconv.Atoi(parts[0])
			minute, _ := strconv.Atoi(parts[1])
			second, _ := strconv.Atoi(parts[2])
			return time.Date(d.Year, time.Month(d.Month), d.Day, hour, minute, second, 0, time.UTC)
		}
	}

	// Default to noon if no time specified
	return time.Date(d.Year, time.Month(d.Month), d.Day, 12, 0, 0, 0, time.UTC)
}

// StandardizedFilename generates a standardized filename based on date info
// Format: YYYY-MM-DD_120000_description.ext
func (d *DateInfo) StandardizedFilename(description string, ext string) string {
	// Extract time component or use default
	timeStr := "120000" // Default to noon
	if d.Time != "" {
		timeStr = strings.ReplaceAll(d.Time, ":", "")
	}

	// Clean description: remove existing date patterns, trim spaces, replace spaces with underscores
	desc := description
	desc = regexp.MustCompile(`^\d{4}[-_]?\d{0,2}[-_]?\d{0,2}_?`).ReplaceAllString(desc, "")
	desc = regexp.MustCompile(`^\d{6}_?`).ReplaceAllString(desc, "")
	desc = strings.TrimSpace(desc)
	desc = strings.ReplaceAll(desc, " ", "_")

	if desc == "" {
		desc = "photo"
	}

	return fmt.Sprintf("%04d-%02d-%02d_%s_%s%s", d.Year, d.Month, d.Day, timeStr, desc, ext)
}

// GetDirectoryPath returns the standardized directory path for this date
// Format: YYYY/YYYY-MM/
func (d *DateInfo) GetDirectoryPath() string {
	return fmt.Sprintf("%04d/%04d-%02d", d.Year, d.Year, d.Month)
}
