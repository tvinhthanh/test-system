package autotest

import (
	"fmt"
	"slices"
	"strings"
)

// The compatibility matrix (QĐ-19 tier 2): a run's cases run once per
// browser × device variant, written "browser/device". "" is the default —
// Chromium on a desktop — and keeps the test identity runs had before the
// matrix, so "chromium/desktop" is stored as "".
var (
	Browsers = []string{"chromium", "firefox", "webkit"}
	Devices  = []string{"desktop", "phone", "tablet"}
)

// MaxVariants a run may ask for; MaxRunRows caps cases × variants.
const (
	MaxVariants = 9
	MaxRunRows  = 300
)

// Variant is where a case runs.
type Variant struct {
	Browser string `json:"browser"`
	Device  string `json:"device"`
}

// ParseVariant reads "browser/device" ("" is the default).
func ParseVariant(s string) (Variant, error) {
	if s == "" {
		return Variant{Browser: "chromium", Device: "desktop"}, nil
	}
	b, d, ok := strings.Cut(s, "/")
	if !ok || !slices.Contains(Browsers, b) || !slices.Contains(Devices, d) {
		return Variant{}, fmt.Errorf("variant %q is browser/device: browser one of %s, device one of %s",
			s, strings.Join(Browsers, ", "), strings.Join(Devices, ", "))
	}
	return Variant{Browser: b, Device: d}, nil
}

// String is the stored form: "" for the default.
func (v Variant) String() string {
	if v.Browser == "chromium" && v.Device == "desktop" {
		return ""
	}
	return v.Browser + "/" + v.Device
}

// Label names a variant for people: "chromium/desktop" for the default.
func Label(variant string) string {
	if variant == "" {
		return "chromium/desktop"
	}
	return variant
}

// cleanVariants validates a run's variants, in order, without repeats;
// nothing asked is the default alone.
func cleanVariants(in []string) ([]string, error) {
	if len(in) == 0 {
		return []string{""}, nil
	}
	if len(in) > MaxVariants {
		return nil, invalid("at most %d variants", MaxVariants)
	}
	out := []string{}
	for _, s := range in {
		v, err := ParseVariant(strings.ToLower(strings.TrimSpace(s)))
		if err != nil {
			return nil, invalid("%v", err)
		}
		if k := v.String(); !slices.Contains(out, k) {
			out = append(out, k)
		}
	}
	return out, nil
}
