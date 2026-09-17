//go:build !linux && !windows

package metric

// residentBytes is not implemented on this platform; the gauge reads 0 and
// says so in its help text.
func residentBytes() uint64 { return 0 }
