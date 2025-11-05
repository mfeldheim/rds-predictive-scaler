package scaler

import (
	"fmt"
	"strconv"
	"strings"
)

func containsString(list []string, str string) bool {
	for _, s := range list {
		if s == str {
			return true
		}
	}
	return false
}

func splitAndTrimStrings(input, sep string) []string {
	items := strings.Split(input, sep)
	for i, item := range items {
		items[i] = strings.TrimSpace(item)
	}
	return items
}

func isBoostHour(hour int, scaleOutHours []int) bool {
	for _, h := range scaleOutHours {
		if hour == h {
			return true
		}
	}
	return false
}

func parseBoostHours(scaleOutHoursStr string) ([]int, error) {
	if scaleOutHoursStr == "" {
		return nil, nil // Return nil to indicate no boost hours specified
	}

	hoursStr := splitAndTrimStrings(scaleOutHoursStr, ",")
	scaleOutHours := make([]int, 0, len(hoursStr))
	for _, hourStr := range hoursStr {
		hour, err := strconv.Atoi(hourStr)
		if err != nil {
			return nil, fmt.Errorf("invalid hour: %s", hourStr)
		}
		scaleOutHours = append(scaleOutHours, hour)
	}
	return scaleOutHours, nil
}

// parseReaderInstanceClasses parses a comma-separated string like "r8g.xlarge,r7g.xlarge,r6g.xlarge"
// into a slice of instance class strings. Order matters: first class is preferred for on-demand instances.
func parseReaderInstanceClasses(configStr string) ([]string, error) {
	if configStr == "" {
		return nil, nil // Return nil to indicate no reader instance classes specified
	}

	instanceClasses := splitAndTrimStrings(configStr, ",")

	// Validate that we have at least one instance class
	if len(instanceClasses) == 0 {
		return nil, fmt.Errorf("reader instance classes configuration is empty")
	}

	// Validate each instance class is not empty
	for i, class := range instanceClasses {
		if class == "" {
			return nil, fmt.Errorf("instance class at position %d is empty", i)
		}
	}

	return instanceClasses, nil
}
