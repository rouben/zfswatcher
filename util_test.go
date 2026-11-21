package main

import (
	"strings"
	"testing"
	"time"
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
