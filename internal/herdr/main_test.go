package herdr

import (
	"os"
	"testing"
)

// hsehtest imports herdr, so this package isolates its own config dir (see hsehtest.Main).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hseh-test-config-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_CONFIG_DIR", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
