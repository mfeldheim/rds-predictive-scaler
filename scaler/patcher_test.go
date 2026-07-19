package scaler

import (
	"testing"
	"time"
)

// parseWindowTime and isInMaintenanceWindow are pure functions in patcher.go

func TestParseWindowTime(t *testing.T) {
	// Anchor: use a known Monday (2024-01-08) at noon UTC
	ref := time.Date(2024, 1, 8, 12, 0, 0, 0, time.UTC) // Monday

	cases := []struct {
		input       string
		wantWeekday time.Weekday
		wantHour    int
		wantMinute  int
		wantErr     bool
	}{
		{"mon:05:00", time.Monday, 5, 0, false},
		{"wed:14:30", time.Wednesday, 14, 30, false},
		{"sun:00:00", time.Sunday, 0, 0, false},
		{"sat:23:59", time.Saturday, 23, 59, false},
		{"invalid", 0, 0, 0, true},
		{"xxx:05:00", 0, 0, 0, true},
	}

	for _, c := range cases {
		got, err := parseWindowTime(c.input, ref)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseWindowTime(%q) expected error, got nil", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseWindowTime(%q) unexpected error: %v", c.input, err)
			continue
		}
		if got.Weekday() != c.wantWeekday {
			t.Errorf("parseWindowTime(%q).Weekday() = %v, want %v", c.input, got.Weekday(), c.wantWeekday)
		}
		if got.Hour() != c.wantHour {
			t.Errorf("parseWindowTime(%q).Hour() = %d, want %d", c.input, got.Hour(), c.wantHour)
		}
		if got.Minute() != c.wantMinute {
			t.Errorf("parseWindowTime(%q).Minute() = %d, want %d", c.input, got.Minute(), c.wantMinute)
		}
	}
}

func TestIsInMaintenanceWindow(t *testing.T) {
	cases := []struct {
		desc     string
		window   string
		t        time.Time
		expected bool
	}{
		{
			desc:     "exactly at window start",
			window:   "mon:05:00-mon:06:00",
			t:        time.Date(2024, 1, 8, 5, 0, 0, 0, time.UTC), // Monday
			expected: true,
		},
		{
			desc:     "inside window",
			window:   "mon:05:00-mon:06:00",
			t:        time.Date(2024, 1, 8, 5, 30, 0, 0, time.UTC),
			expected: true,
		},
		{
			desc:     "at window end (exclusive)",
			window:   "mon:05:00-mon:06:00",
			t:        time.Date(2024, 1, 8, 6, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			desc:     "before window",
			window:   "mon:05:00-mon:06:00",
			t:        time.Date(2024, 1, 8, 4, 59, 0, 0, time.UTC),
			expected: false,
		},
		{
			desc:     "after window same day",
			window:   "mon:05:00-mon:06:00",
			t:        time.Date(2024, 1, 8, 7, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			desc:     "different day of week",
			window:   "wed:05:00-wed:06:00",
			t:        time.Date(2024, 1, 8, 5, 30, 0, 0, time.UTC), // Monday
			expected: false,
		},
		{
			desc:     "window spanning midnight",
			window:   "mon:23:30-tue:00:30",
			t:        time.Date(2024, 1, 8, 23, 45, 0, 0, time.UTC),
			expected: true,
		},
		{
			desc:     "window spanning midnight - in second day portion",
			window:   "mon:23:30-tue:00:30",
			t:        time.Date(2024, 1, 9, 0, 15, 0, 0, time.UTC), // Tuesday
			expected: true,
		},
		{
			desc:     "malformed window",
			window:   "not-a-window",
			t:        time.Date(2024, 1, 8, 5, 0, 0, 0, time.UTC),
			expected: false,
		},
		{
			desc:     "saturday window - in window",
			window:   "sat:02:00-sat:03:00",
			t:        time.Date(2024, 1, 13, 2, 30, 0, 0, time.UTC), // Saturday
			expected: true,
		},
		{
			desc:     "saturday window - not in window",
			window:   "sat:02:00-sat:03:00",
			t:        time.Date(2024, 1, 13, 1, 59, 0, 0, time.UTC),
			expected: false,
		},
	}

	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			got := isInMaintenanceWindow(c.window, c.t)
			if got != c.expected {
				t.Errorf("isInMaintenanceWindow(%q, %v) = %v, want %v", c.window, c.t, got, c.expected)
			}
		})
	}
}
