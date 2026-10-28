package handlers

import (
	"os/exec"
	"testing"
)

// stubBinary returns the path of a small system program (true, false, echo)
// that tests use in place of ffmpeg or ffprobe. Unix systems have them in
// /bin or /usr/bin; on Windows Git for Windows usually puts them on PATH.
// Without one the test cannot run and is skipped.
func stubBinary(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("no %q binary on PATH: %v", name, err)
	}
	return path
}
