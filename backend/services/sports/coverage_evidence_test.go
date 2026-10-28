package sports

import (
	"errors"
	"io/fs"
	"os"
	"testing"
)

// readCoverageEvidence reads recorded provider evidence from
// docs/sports-coverage. Maintainers keep that directory outside the
// repository, so tests that need it are skipped when it is absent instead of
// failing every checkout.
func readCoverageEvidence(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("%s is not present; provider coverage evidence is recorded outside the repository", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
