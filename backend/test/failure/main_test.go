package failure

import (
	"os"
	"testing"

	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}
