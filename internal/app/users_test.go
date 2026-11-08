package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/A1nar-S/advertisiment-monitoring/internal/subscriptions"
)

// The example users file must stay valid against the real sources (the
// ss.com codebook), since it is what people copy.
func TestExampleUsersFileIsValid(t *testing.T) {
	if err := subscriptions.ValidateFile(KnownSources(0, 0), "../../config/users.example.yaml"); err != nil {
		t.Fatal(err)
	}
}

func TestValidateFileReportsTypos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.yaml")
	err := os.WriteFile(path, []byte(`
users:
  - name: me
    channels: [{kind: discord, target: "1"}]
    searches:
      - {name: a, feeds: [transport/cars], filters: {make: [Skodda]}}
      - {name: b, feeds: [transport/cars], filters: {gearbox: [Automatic]}}
`), 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = subscriptions.ValidateFile(KnownSources(0, 0), path)
	if err == nil || !strings.Contains(err.Error(), `"Skoda"`) || !strings.Contains(err.Error(), `search "b"`) {
		t.Errorf("err = %v, want both searches reported with a suggestion", err)
	}
}
