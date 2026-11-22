package notifier

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoggerFileMode(t *testing.T) {
	name := filepath.Join(t.TempDir(), "test.log")
	n := New()
	if err := n.AddLoggerFile(DEBUG, name); err != nil {
		t.Fatal(err)
	}
	if err := n.Send(INFO, "hello"); err != nil {
		t.Fatal(err)
	}
	<-n.Close()

	fi, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	// The umask may only remove bits.
	if extra := fi.Mode().Perm() &^ 0o640; extra != 0 {
		t.Errorf("mode %v has bits beyond 0640", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(name)
	if !strings.Contains(string(b), "hello") {
		t.Errorf("log = %q", b)
	}
}
