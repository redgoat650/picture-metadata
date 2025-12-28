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

// ExtractDirectoryContext extracts meaningful directory names from a path
// and returns them concatenated with underscores, cleaned of dates and special chars
func ExtractDirectoryContext(fullPath, sourceRoot string) string {
	// Normalize paths - remove trailing slashes
	sourceRoot = strings.TrimRight(sourceRoot, "/")
	fullPath = strings.TrimRight(fullPath, "/")

	// Remove the source root prefix to get relative path
	relPath := fullPath
	if strings.HasPrefix(fullPath, sourceRoot) {
		relPath = strings.TrimPrefix(fullPath, sourceRoot)
		relPath = strings.TrimPrefix(relPath, "/")
	}

	// Split into directory components (exclude the filename itself)
	dirPath := filepath.Dir(relPath)
	if dirPath == "." || dirPath == "" {
		return ""
	}

	parts := strings.Split(dirPath, "/")

	var contextParts []string
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}

		// Clean the directory name
		cleaned := cleanDirectoryName(part)
		if cleaned != "" {
			contextParts = append(contextParts, cleaned)
		}
	}

	return strings.Join(contextParts, "_")
}

// cleanDirectoryName removes date patterns and cleans up a directory name
func cleanDirectoryName(dir string) string {
	// Remove common date patterns at the start
	dir = regexp.MustCompile(`^\d{4}[-_]\d{2}[-_]\d{2}`).ReplaceAllString(dir, "") // YYYY-MM-DD or YYYY_MM_DD
	dir = regexp.MustCompile(`^\d{8}`).ReplaceAllString(dir, "")                   // YYYYMMDD
	dir = regexp.MustCompile(`^\d{6}`).ReplaceAllString(dir, "")                   // YYMMDD or YYMM
	dir = regexp.MustCompile(`^\d{4}[-_]`).ReplaceAllString(dir, "")               // YYYY_ or YYYY-
	dir = regexp.MustCompile(`^\d{4}\s+`).ReplaceAllString(dir, "")                // YYYY followed by space

	// Remove decade ranges like "2010-2019", "1980-1989" (anywhere in the string)
	dir = regexp.MustCompile(`\d{4}-\d{4}`).ReplaceAllString(dir, "")

	// Remove "and before" or similar suffix patterns
	dir = regexp.MustCompile(`\s+and\s+(before|after)$`).ReplaceAllString(dir, "")

	// Trim underscores and spaces
	dir = strings.Trim(dir, "_- ")

	// Replace spaces with underscores
	dir = strings.ReplaceAll(dir, " ", "_")

	// Remove multiple consecutive underscores
	dir = regexp.MustCompile(`_+`).ReplaceAllString(dir, "_")

	// Clean special characters but keep alphanumeric and underscores
	dir = regexp.MustCompile(`[^a-zA-Z0-9_]`).ReplaceAllString(dir, "_")

	// Remove multiple consecutive underscores again after cleaning
	dir = regexp.MustCompile(`_+`).ReplaceAllString(dir, "_")

	// Trim underscores from start and end
	dir = strings.Trim(dir, "_")

	return dir
}

// ParseDateFromFilename extracts date information from photo filenames or directory paths
// Supports formats like:
// - YYYYMMDD_description.jpg
// - YYYY_MM_DD_description.jpg
// - YYYY-MM-DD description.jpg (with hyphens)
// - YYYY-MM-DD HH.MM.SS.jpg (with time)
// - YYYY_description.jpg
// - YYMMDD_description.jpg (for years 19XX or 20XX)
// - YYMM_description.jpg (for years 19XX or 20XX, defaults to 1st of month)
// Also checks parent directory names for date patterns
func ParseDateFromFilename(filename string) (*DateInfo, error) {
	base := filepath.Base(filename)
	name := strings.TrimSuffix(base, filepath.Ext(base))

	// Also check the full path (parent directories may contain dates)
	fullPath := filename

	// Try various date patterns
	// Order matters! Check more specific patterns first
	patterns := []struct {
		regex   *regexp.Regexp
		extract func([]string) (*DateInfo, error)
	}{
		{
			// YYYY-MM-DD HH.MM.SS format (with time, spaces, hyphens, and periods)
			regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})\s+(\d{2})\.(\d{2})\.(\d{2})`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				day, _ := strconv.Atoi(matches[3])
				hour, _ := strconv.Atoi(matches[4])
				minute, _ := strconv.Atoi(matches[5])
				second, _ := strconv.Atoi(matches[6])
				timeStr := fmt.Sprintf("%02d:%02d:%02d", hour, minute, second)
				return &DateInfo{Year: year, Month: month, Day: day, Time: timeStr, Original: base}, nil
			},
		},
		{
			// YYYY-MM-DD format (with hyphens)
			regexp.MustCompile(`(\d{4})-(\d{2})-(\d{2})`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				day, _ := strconv.Atoi(matches[3])
				return &DateInfo{Year: year, Month: month, Day: day, Original: base}, nil
			},
		},
		{
			// YYYY_MM_DD format (with underscores)
			regexp.MustCompile(`(\d{4})_(\d{2})_(\d{2})`),
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
			// YYYY_MM format in path or filename (e.g., 2019_11_identity)
			regexp.MustCompile(`(\d{4})_(\d{2})(?:_|\D|$)`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				month, _ := strconv.Atoi(matches[2])
				return &DateInfo{Year: year, Month: month, Day: 1, Original: base}, nil
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
			// YYYY only format (year only, no specific month/day) - matches YYYY_ or YYYY/ in path
			regexp.MustCompile(`[/\\](\d{4})(?:_|/|\\)`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				// Default to January 1st when only year is available
				return &DateInfo{Year: year, Month: 1, Day: 1, Original: base}, nil
			},
		},
		{
			// "YYYY and before" pattern in directory paths (e.g., "1949 and before")
			regexp.MustCompile(`(\d{4})\s+and\s+before`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				// Use the specified year as the default
				return &DateInfo{Year: year, Month: 1, Day: 1, Original: base}, nil
			},
		},
		{
			// YYYY at start of filename or directory (e.g., "1933Lilian", "1903_Ivan")
			regexp.MustCompile(`(?:^|[/\\])(\d{4})[A-Za-z_]`),
			func(matches []string) (*DateInfo, error) {
				year, _ := strconv.Atoi(matches[1])
				// Default to January 1st when only year is available
				return &DateInfo{Year: year, Month: 1, Day: 1, Original: base}, nil
			},
		},
	}

	// Try to match patterns in both the filename and the full path
	// First try the filename, then the full path
	searchStrings := []string{name, fullPath}

	for _, searchStr := range searchStrings {
		for _, pattern := range patterns {
			if matches := pattern.regex.FindStringSubmatch(searchStr); matches != nil {
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
// Format: YYYY-MM-DD_description.ext (time only included if not default)
// Format with time: YYYY-MM-DD_HHMMSS_description.ext
func (d *DateInfo) StandardizedFilename(description string, ext string) string {
	// Clean description: remove existing date patterns, trim spaces, replace spaces with underscores
	desc := description
	desc = regexp.MustCompile(`^\d{4}[-_]?\d{0,2}[-_]?\d{0,2}_?`).ReplaceAllString(desc, "")
	desc = regexp.MustCompile(`^\d{6}_?`).ReplaceAllString(desc, "")
	desc = strings.TrimSpace(desc)
	desc = strings.ReplaceAll(desc, " ", "_")

	if desc == "" {
		desc = "photo"
	}

	// Only include time if it's not the default noon time
	if d.Time != "" && d.Time != "12:00:00" {
		timeStr := strings.ReplaceAll(d.Time, ":", "")
		return fmt.Sprintf("%04d-%02d-%02d_%s_%s%s", d.Year, d.Month, d.Day, timeStr, desc, ext)
	}

	return fmt.Sprintf("%04d-%02d-%02d_%s%s", d.Year, d.Month, d.Day, desc, ext)
}

// GetDirectoryPath returns the standardized directory path for this date
// Format: YYYY/YYYY-MM/
func (d *DateInfo) GetDirectoryPath() string {
	return fmt.Sprintf("%04d/%04d-%02d", d.Year, d.Year, d.Month)
}
