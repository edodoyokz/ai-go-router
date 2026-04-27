package updater

import "testing"

func TestIsNewer(t *testing.T) {
	cases := []struct {
		current   string
		candidate string
		want      bool
	}{
		// Candidate is newer
		{"v1.0.0", "v1.0.1", true},
		{"v1.0.0", "v1.1.0", true},
		{"v1.0.0", "v2.0.0", true},
		{"v0.9.9", "v1.0.0", true},
		// Same version
		{"v1.0.0", "v1.0.0", false},
		// Candidate is older
		{"v1.1.0", "v1.0.9", false},
		{"v2.0.0", "v1.9.9", false},
		// Without "v" prefix
		{"1.0.0", "1.0.1", true},
		{"1.5.0", "1.4.9", false},
		// Mixed prefix
		{"v1.0.0", "1.0.1", true},
		// The bug case: string comparison "1.0.9" > "1.0.10" lexicographically, but semver-wise 1.0.10 > 1.0.9
		{"v1.0.9", "v1.0.10", true},
		{"v1.9.0", "v1.10.0", true},
		// Pre-release suffix ignored
		{"v1.0.0", "v1.0.1-beta", true},
		{"v1.0.1-beta", "v1.0.1", false},
	}

	for _, tc := range cases {
		got := isNewer(tc.current, tc.candidate)
		if got != tc.want {
			t.Errorf("isNewer(%q, %q) = %v, want %v", tc.current, tc.candidate, got, tc.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	cases := []struct {
		input string
		want  [3]int
		ok    bool
	}{
		{"v1.2.3", [3]int{1, 2, 3}, true},
		{"1.2.3", [3]int{1, 2, 3}, true},
		{"0.9.9", [3]int{0, 9, 9}, true},
		{"10.20.30", [3]int{10, 20, 30}, true},
		{"v1.0.1-beta", [3]int{1, 0, 1}, true},
		{"v1.0", [3]int{}, false},
		{"not-semver", [3]int{}, false},
	}

	for _, tc := range cases {
		got, err := parseSemver(tc.input)
		if tc.ok && err != nil {
			t.Errorf("parseSemver(%q) unexpected error: %v", tc.input, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("parseSemver(%q) expected error, got nil", tc.input)
		}
		if tc.ok && got != tc.want {
			t.Errorf("parseSemver(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
