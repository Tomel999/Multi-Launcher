package version

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1.2.3", "v1.2.3"},
		{"v1.2.3", "v1.2.3"},
		{"  v1.2.3  ", "v1.2.3"},
		{"", ""},
		{"   ", ""},
		{"dev", "vdev"},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsValid(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"v1.2.3", true},
		{"1.2.3", true},
		{"v1.2.3-beta.1", true},
		{"v1.2.3+meta", true},
		{"v1.2", false},
		{"dev", false},
		{"", false},
		{"garbage", false},
	}
	for _, c := range cases {
		if got := IsValid(c.in); got != c.want {
			t.Errorf("IsValid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    bool
	}{
		// newer
		{"newer patch", "v1.2.3", "v1.2.4", true},
		{"newer minor", "v1.2.3", "v1.3.0", true},
		{"newer major", "v1.9.9", "v2.0.0", true},
		{"newer without v prefix on either side", "1.2.3", "1.2.4", true},
		{"newer mixed prefixes", "v1.2.3", "1.10.0", true},

		// equal -> never an update
		{"equal", "v1.2.3", "v1.2.3", false},
		{"equal with mixed prefix", "1.2.3", "v1.2.3", false},
		{"equal with build metadata", "v1.2.3+a", "v1.2.3+b", false},

		// older
		{"older patch", "v1.2.4", "v1.2.3", false},
		{"older minor", "v1.3.0", "v1.2.9", false},
		{"older major", "v2.0.0", "v1.99.99", false},

		// prerelease ordering
		{"prerelease below release", "v1.2.3-beta.1", "v1.2.3", true},
		{"release above prerelease", "v1.2.3", "v1.2.3-beta.1", false},
		{"prerelease progression", "v1.2.3-alpha.1", "v1.2.3-alpha.2", true},

		// malformed input
		{"malformed latest is ignored", "v1.0.0", "not-a-version", false},
		{"malformed latest empty", "v1.0.0", "", false},
		{"dev current updates to real release", "dev", "v1.0.0", true},
		{"both malformed", "dev", "garbage", false},
		{"both empty", "", "", false},
		{"empty current updates to release", "", "v0.0.1", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsNewer(c.current, c.latest); got != c.want {
				t.Errorf("IsNewer(%q, %q) = %v, want %v", c.current, c.latest, got, c.want)
			}
		})
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		current, latest string
		want            int
	}{
		{"v1.0.0", "v1.0.1", -1},
		{"v1.0.1", "v1.0.0", 1},
		{"v1.0.0", "v1.0.0", 0},
		{"garbage", "garbage", 0},
		// A valid version always outranks an invalid one.
		{"v1.0.0", "garbage", 1},
		{"garbage", "v1.0.0", -1},
	}
	for _, c := range cases {
		if got := Compare(c.current, c.latest); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.current, c.latest, got, c.want)
		}
	}
}

func TestIsDev(t *testing.T) {
	if !IsDev(Version) {
		t.Errorf("default Version %q should be treated as a dev build", Version)
	}
	if IsDev("v1.0.0") {
		t.Error("v1.0.0 should not be treated as a dev build")
	}
}
