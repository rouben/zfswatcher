package main

import (
	"strings"
	"testing"
)

// The inputs below are not captured from a real system. They are assembled
// from the format strings in OpenZFS cmd/zpool/zpool_main.c (print_scan_status,
// print_removal_status, print_checkpoint_status, print_raidz_expand_status,
// print_status_config) to cover situations that are hard to reproduce.

const (
	synthHeader = "  pool: tank\n state: DEGRADED\n"
	synthConfig = "config:\n\n" +
		"\tNAME        STATE     READ WRITE CKSUM\n" +
		"\ttank        DEGRADED     0     0     0\n" +
		"\t  mirror-0  DEGRADED     0     0     0\n" +
		"\t    sda     ONLINE       0     0     0\n" +
		"\t    sdb     ONLINE       0     0     0  (resilvering)\n" +
		"\nerrors: No known data errors\n"
)

func parseSingle(t *testing.T, in string) *PoolType {
	t.Helper()
	pools, err := parseZpoolStatus(in)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("got %d pools, want 1", len(pools))
	}
	return pools[0]
}

func TestParseZpoolStatusResilverInProgress(t *testing.T) {
	p := parseSingle(t, synthHeader+
		"status: One or more devices is currently being resilvered.  The pool will\n"+
		"\tcontinue to function, possibly in a degraded state.\n"+
		"action: Wait for the resilver to complete.\n"+
		"  scan: resilver in progress since Sat Nov 21 16:00:00 2026\n"+
		"\t1.20G / 3.40G scanned at 100M/s, 900M / 3.10G issued at 80M/s\n"+
		"\t300M resilvered, 29.03% done, 00:00:30 to go\n"+
		synthConfig)

	if !strings.HasPrefix(p.scan, "resilver in progress since") ||
		!strings.Contains(p.scan, "300M resilvered, 29.03% done") {
		t.Errorf("unexpected scan: %q", p.scan)
	}
	if p.action != "Wait for the resilver to complete." {
		t.Errorf("unexpected action: %q", p.action)
	}
	var sdb *DevEntry
	for _, d := range p.devs {
		if d.name == "sdb" {
			sdb = d
		}
	}
	if sdb == nil || sdb.state != "ONLINE" || sdb.rest != "(resilvering)" {
		t.Errorf("unexpected sdb entry: %+v", sdb)
	}
}

func TestParseZpoolStatusOptionalSections(t *testing.T) {
	tests := []struct {
		name  string
		input string
		extra string // substring expected in PoolType.extra
	}{
		{
			"checkpoint",
			synthHeader + "  scan: scrub repaired 0B in 00:00:01 with 0 errors on Sat Nov 21 16:00:00 2026\n" +
				"checkpoint: created Sat Nov 21 16:00:00 2026, consumes 1.2G\n" + synthConfig,
			"consumes 1.2G",
		},
		{
			"removal",
			synthHeader + "  scan: none requested\n" +
				"remove: Removal of vdev 1 copied 1.2G in 0h3m, completed on Sat Nov 21 16:00:00 2026\n" +
				"\t12.3K memory used for removed device mappings\n" + synthConfig,
			"12.3K memory used",
		},
		{
			"raidz expansion",
			synthHeader + "  scan: none requested\n" +
				"expand: expansion of raidz1-0 in progress since Sat Nov 21 16:00:00 2026\n" +
				"\t1.2G / 3.4G copied at 100M/s, 35.29% done, 00:00:22 to go\n" + synthConfig,
			"35.29% done",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := parseSingle(t, tt.input)
			if !strings.Contains(p.extra, tt.extra) {
				t.Errorf("extra = %q, want it to contain %q", p.extra, tt.extra)
			}
			if len(p.devs) != 4 || p.errors != "No known data errors" {
				t.Errorf("config not parsed after the section: %d devs, errors %q", len(p.devs), p.errors)
			}
		})
	}
}

func TestParseZpoolStatusWithoutAction(t *testing.T) {
	p := parseSingle(t, synthHeader+"status: Some devices are slow.\n  scan: none requested\n"+synthConfig)
	if p.status != "Some devices are slow." || p.scan != "none requested" {
		t.Errorf("unexpected status=%q scan=%q", p.status, p.scan)
	}
}

func TestParseZpoolStatusDataErrors(t *testing.T) {
	p := parseSingle(t, "  pool: tank\n state: ONLINE\n  scan: none requested\n"+
		"config:\n\n\tNAME  STATE     READ WRITE CKSUM\n\ttank  ONLINE       0     0     0\n"+
		"\nerrors: 1 data errors, use '-v' for a list\n")
	if p.errors != "1 data errors, use '-v' for a list" {
		t.Errorf("unexpected errors: %q", p.errors)
	}
}
