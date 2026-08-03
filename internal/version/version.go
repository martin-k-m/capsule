// Package version holds capsule's own version.
//
// It sits below both the CLI, which prints it, and the capsule.toml reader,
// which compares a file's `capsule` requirement against it. Either one owning it
// would force the other to import a package it has no other business with.
package version

import (
	"fmt"
	"strconv"
	"strings"
)

// Current is stamped at build time with
// -ldflags "-X github.com/martin-k-m/capsule/internal/version.Current=v0.2.0".
// A build from source keeps the -dev suffix, so `capsule version` never claims
// to be a release it is not.
var Current = "1.0.0"

// Triple is a parsed major.minor.patch.
type Triple [3]int

// Parse reads a version, tolerating a leading "v" and stopping at a
// "-prerelease" or "+build" suffix.
//
// Omitted components are zero, so "0.2" and "0.2.0" parse alike: a capsule.toml
// names the series it needs, not a patch. Ignoring the prerelease suffix means
// 0.2.0-dev satisfies ">=0.2", which is what you want from a build of exactly
// that series and avoids reimplementing semver precedence for a tool this size.
func Parse(s string) (Triple, error) {
	var t Triple
	bad := fmt.Errorf("%q is not a version, write MAJOR.MINOR or MAJOR.MINOR.PATCH", s)

	v := strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > len(t) {
		return t, bad
	}
	for i, p := range parts {
		// The prerelease suffix is already gone, so no part can carry a sign and
		// Atoi cannot hand back a negative here.
		n, err := strconv.Atoi(p)
		if err != nil {
			return t, bad
		}
		t[i] = n
	}
	return t, nil
}

// Less reports whether t orders before o.
func (t Triple) Less(o Triple) bool {
	for i := range t {
		if t[i] != o[i] {
			return t[i] < o[i]
		}
	}
	return false
}

// Series is Current as "MAJOR.MINOR", the granularity a capsule.toml requirement
// is written at. A stamp this package cannot read falls back to "0.0", which
// every build satisfies: a mangled -ldflags value should not make the configs
// `capsule init` writes unreadable.
func Series() string {
	t, err := Parse(Current)
	if err != nil {
		return "0.0"
	}
	return fmt.Sprintf("%d.%d", t[0], t[1])
}
