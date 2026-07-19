package scaler

import (
	"testing"
)

func TestContainsString(t *testing.T) {
	cases := []struct {
		list     []string
		str      string
		expected bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{[]string{"deleting", "modifying"}, "modifying", true},
	}
	for _, c := range cases {
		if got := containsString(c.list, c.str); got != c.expected {
			t.Errorf("containsString(%v, %q) = %v, want %v", c.list, c.str, got, c.expected)
		}
	}
}

func TestParseBoostHours(t *testing.T) {
	cases := []struct {
		input    string
		expected []int
		wantErr  bool
	}{
		{"", nil, false},
		{"8,9,10", []int{8, 9, 10}, false},
		{" 8 , 9 , 10 ", []int{8, 9, 10}, false},
		{"8", []int{8}, false},
		{"abc", nil, true},
	}
	for _, c := range cases {
		got, err := parseBoostHours(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseBoostHours(%q) expected error, got nil", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseBoostHours(%q) unexpected error: %v", c.input, err)
			continue
		}
		if len(got) != len(c.expected) {
			t.Errorf("parseBoostHours(%q) = %v, want %v", c.input, got, c.expected)
			continue
		}
		for i := range got {
			if got[i] != c.expected[i] {
				t.Errorf("parseBoostHours(%q)[%d] = %d, want %d", c.input, i, got[i], c.expected[i])
			}
		}
	}
}

func TestIsBoostHour(t *testing.T) {
	hours := []int{8, 9, 10, 18}
	cases := []struct {
		hour     int
		expected bool
	}{
		{8, true},
		{9, true},
		{18, true},
		{7, false},
		{11, false},
		{0, false},
	}
	for _, c := range cases {
		if got := isBoostHour(c.hour, hours); got != c.expected {
			t.Errorf("isBoostHour(%d, %v) = %v, want %v", c.hour, hours, got, c.expected)
		}
	}
}

func TestParseReaderInstanceClasses(t *testing.T) {
	cases := []struct {
		input    string
		expected []string
		wantErr  bool
	}{
		{"", nil, false},
		{"r8g.xlarge", []string{"r8g.xlarge"}, false},
		{"r8g.xlarge,r7g.xlarge,r6g.xlarge", []string{"r8g.xlarge", "r7g.xlarge", "r6g.xlarge"}, false},
		{" r8g.xlarge , r7g.xlarge ", []string{"r8g.xlarge", "r7g.xlarge"}, false},
	}
	for _, c := range cases {
		got, err := parseReaderInstanceClasses(c.input)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseReaderInstanceClasses(%q) expected error, got nil", c.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseReaderInstanceClasses(%q) unexpected error: %v", c.input, err)
			continue
		}
		if len(got) != len(c.expected) {
			t.Errorf("parseReaderInstanceClasses(%q) = %v, want %v", c.input, got, c.expected)
			continue
		}
		for i := range got {
			if got[i] != c.expected[i] {
				t.Errorf("parseReaderInstanceClasses(%q)[%d] = %q, want %q", c.input, i, got[i], c.expected[i])
			}
		}
	}
}

func TestNormalizeInstanceClass(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"r8g.xlarge", "db.r8g.xlarge"},
		{"db.r8g.xlarge", "db.r8g.xlarge"},
		{"db.t3.medium", "db.t3.medium"},
		{"t3.medium", "db.t3.medium"},
	}
	for _, c := range cases {
		if got := normalizeInstanceClass(c.input); got != c.expected {
			t.Errorf("normalizeInstanceClass(%q) = %q, want %q", c.input, got, c.expected)
		}
	}
}

func TestIsDeletableStatus(t *testing.T) {
	cases := []struct {
		status   string
		expected bool
	}{
		{"available", true},
		{"backing-up", true},
		{"deleting", false},
		{"modifying", false},
		{"maintenance", false},
		{"rebooting", false},
		{"creating", true},
		{"stopped", true},
	}
	for _, c := range cases {
		if got := isDeletableStatus(c.status); got != c.expected {
			t.Errorf("isDeletableStatus(%q) = %v, want %v", c.status, got, c.expected)
		}
	}
}

func TestSplitAndTrimStrings(t *testing.T) {
	cases := []struct {
		input    string
		sep      string
		expected []string
	}{
		{"a,b,c", ",", []string{"a", "b", "c"}},
		{" a , b , c ", ",", []string{"a", "b", "c"}},
		{"single", ",", []string{"single"}},
		{"", ",", []string{""}},
	}
	for _, c := range cases {
		got := splitAndTrimStrings(c.input, c.sep)
		if len(got) != len(c.expected) {
			t.Errorf("splitAndTrimStrings(%q, %q) = %v, want %v", c.input, c.sep, got, c.expected)
			continue
		}
		for i := range got {
			if got[i] != c.expected[i] {
				t.Errorf("splitAndTrimStrings(%q, %q)[%d] = %q, want %q", c.input, c.sep, i, got[i], c.expected[i])
			}
		}
	}
}
