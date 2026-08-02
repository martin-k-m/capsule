package version

import "testing"

func TestParse(t *testing.T) {
	cases := []struct {
		in   string
		want Triple
	}{
		{"0.2", Triple{0, 2, 0}},
		{"0.2.0", Triple{0, 2, 0}},
		{"v1.4.9", Triple{1, 4, 9}},
		{" 1.4.9 ", Triple{1, 4, 9}},
		{"1", Triple{1, 0, 0}},
		// A build of a series counts as that series. 0.2.0-dev satisfies ">=0.2",
		// which is the whole reason a source build can read the configs it writes.
		{"0.2.0-dev", Triple{0, 2, 0}},
		{"v0.2.0-rc1", Triple{0, 2, 0}},
		{"0.2.0+build7", Triple{0, 2, 0}},
	}
	for _, tc := range cases {
		got, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Parse(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestParseRejectsNonVersions(t *testing.T) {
	for _, in := range []string{"", "  ", "v", "latest", "0.x", "1.2.3.4", "0..2", "-1.0"} {
		if got, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %v, want an error", in, got)
		}
	}
}

func TestLess(t *testing.T) {
	cases := []struct {
		a, b Triple
		want bool
	}{
		{Triple{0, 1, 0}, Triple{0, 2, 0}, true},
		{Triple{0, 2, 0}, Triple{0, 2, 0}, false},
		{Triple{0, 3, 0}, Triple{0, 2, 9}, false},
		{Triple{1, 0, 0}, Triple{0, 99, 99}, false},
		{Triple{0, 2, 1}, Triple{0, 2, 2}, true},
	}
	for _, tc := range cases {
		if got := tc.a.Less(tc.b); got != tc.want {
			t.Errorf("%v.Less(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestSeriesIsMajorMinorOfCurrent(t *testing.T) {
	// `capsule init` writes ">=" + Series() and then parses what it wrote, so a
	// Series() this build does not itself satisfy would make init fail on itself.
	want, err := Parse(Series())
	if err != nil {
		t.Fatalf("Series() = %q, which is not a version: %v", Series(), err)
	}
	have, err := Parse(Current)
	if err != nil {
		t.Fatalf("Current = %q, which is not a version: %v", Current, err)
	}
	if have.Less(want) {
		t.Errorf("Series() = %q is newer than Current = %q", Series(), Current)
	}
	if want[2] != 0 {
		t.Errorf("Series() = %q should carry no patch component", Series())
	}
}

func TestSeriesToleratesAnUnreadableStamp(t *testing.T) {
	// A mangled -ldflags value is this binary's problem. It must not make every
	// config `capsule init` writes unreadable.
	defer func(orig string) { Current = orig }(Current)
	Current = "not-a-version"
	if got := Series(); got != "0.0" {
		t.Errorf("Series() = %q with an unreadable stamp, want %q", got, "0.0")
	}
}
