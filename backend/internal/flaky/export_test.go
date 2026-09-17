package flaky

import "time"

// SetNow pins the service clock so a test can move time past the window.
func SetNow(s Service, now func() time.Time) { s.(*service).now = now }
