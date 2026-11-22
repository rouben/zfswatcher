package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tantefred/zfswatcher/notifier"
)

func TestGetCommandOutput(t *testing.T) {
	out, err := getCommandOutput("echo hello")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "hello" {
		t.Errorf("out = %q", out)
	}
}

func TestGetCommandOutputFailure(t *testing.T) {
	if _, err := getCommandOutput("false"); err == nil {
		t.Error("expected an error for a failing command")
	}
}

func TestGetCommandOutputTimeout(t *testing.T) {
	old := commandTimeout
	commandTimeout = 200 * time.Millisecond
	defer func() { commandTimeout = old }()

	start := time.Now()
	_, err := getCommandOutput("sleep 30")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want a timeout error", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("command was not killed in time: %s", elapsed)
	}
}

// captureNotify replaces the global notifier by one which records messages.
func captureNotify(t *testing.T) func() []string {
	t.Helper()
	old := notify
	n := notifier.New()
	var mu sync.Mutex
	var msgs []string
	_ = n.AddLoggerCallback(notifier.DEBUG, func(m *notifier.Msg) {
		if m.MsgType == notifier.MSGTYPE_MESSAGE {
			mu.Lock()
			msgs = append(msgs, fmt.Sprintf("%d %s", m.Severity, m.Text))
			mu.Unlock()
		}
	})
	notify = n
	t.Cleanup(func() { notify = old })
	return func() []string {
		<-n.Close()
		mu.Lock()
		defer mu.Unlock()
		return msgs
	}
}

func TestSetupPidFileError(t *testing.T) {
	get := captureNotify(t)
	cleanup := setupPidFile(filepath.Join(t.TempDir(), "missing", "zfswatcher.pid"))
	cleanup() // must be safe to call
	msgs := get()
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0], fmt.Sprint(int(notifier.ERR))+" ") ||
		!strings.Contains(msgs[0], "cannot write pid file") {
		t.Errorf("messages = %q, want one ERR about the pid file", msgs)
	}
}

func TestSetupPidFileEmpty(t *testing.T) {
	get := captureNotify(t)
	setupPidFile("")()
	if msgs := get(); len(msgs) != 0 {
		t.Errorf("unexpected messages %q", msgs)
	}
}

func TestSetupPidFileOK(t *testing.T) {
	get := captureNotify(t)
	p := filepath.Join(t.TempDir(), "z.pid")
	cleanup := setupPidFile(p)
	b, err := os.ReadFile(p)
	if err != nil || strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Errorf("pid file = %q, %v", b, err)
	}
	cleanup()
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("pid file not removed: %v", err)
	}
	if msgs := get(); len(msgs) != 0 {
		t.Errorf("unexpected messages %q", msgs)
	}
}

func TestCheckRefresh(t *testing.T) {
	for _, c := range []struct {
		sec int
		ok  bool
	}{{0, false}, {-1, false}, {1, true}, {60, true}, {maxRefreshSeconds, true}, {maxRefreshSeconds + 1, false}} {
		if err := checkRefresh(c.sec); (err == nil) != c.ok {
			t.Errorf("checkRefresh(%d) = %v, ok want %v", c.sec, err, c.ok)
		}
	}
}

func TestGetCfgRefreshTooLarge(t *testing.T) {
	old := cfgFile
	defer func() { cfgFile = old }()
	cfgFile = filepath.Join(t.TempDir(), "z.conf")
	if err := os.WriteFile(cfgFile, []byte("[main]\nzpoolstatusrefresh = 99999999\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if getCfg() != nil {
		t.Error("getCfg accepted an excessive refresh interval")
	}
}
