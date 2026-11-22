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

func TestParseZpoolStatusPermanentErrorsList(t *testing.T) {
	pools, err := parseZpoolStatus(readFixture(t, "zpool-status-permanent-errors-v.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 {
		t.Fatalf("got %d pools, want 1", len(pools))
	}
	want := "Permanent errors have been detected in the following files:\n/zfsw_perm/testdata"
	if pools[0].errors != want {
		t.Errorf("errors = %q, want %q", pools[0].errors, want)
	}
	if got := pools[0].devs[1].cksum; got != 504 {
		t.Errorf("cksum = %d, want 504", got)
	}
}

// Empty output and output cut off before the config section are errors.
func TestParseZpoolStatusEmptyAndCutOff(t *testing.T) {
	for name, in := range map[string]string{
		"empty":           "",
		"pool line only":  "  pool: tank\n",
		"cut after state": synthHeader,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseZpoolStatus(in); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// Inputs that must be rejected with an error (and must not crash).
func TestParseZpoolStatusErrors(t *testing.T) {
	tests := []struct {
		name string
		in   string
	}{
		{"garbage first line", "this is not zpool output\n"},
		{"state without pool", " state: ONLINE\n"},
		{"unknown line inside pool", "  pool: tank\n state: ONLINE\nbogus: line\n"},
		{"unknown line inside config", "  pool: tank\n state: ONLINE\nconfig:\n\n\tNAME STATE READ WRITE CKSUM\nbogus\n"},
		{"section after pool end", synthHeader + synthConfig + "\nstatus: late\n"},
		{"blank line instead of pool", "\n  pool: tank\n state: ONLINE\n"},
		{"tab line before any section", "\tindented\n"},
		{"scan directly after pool", "  pool: tank\n scan: none requested\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseZpoolStatus(tt.in); err == nil {
				t.Error("expected an error")
			}
		})
	}
}

// Output cut off inside the config section: the unfinished pool is dropped
// silently and no error is raised.
func TestParseZpoolStatusTruncated(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantPools int
	}{
		{"cut in config", synthHeader + "config:\n\n\tNAME STATE READ WRITE CKSUM\n", 0},
		{"cut before final blank", synthHeader + synthConfig[:len(synthConfig)-1], 0},
		{"complete", synthHeader + synthConfig, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools, err := parseZpoolStatus(tt.in)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(pools) != tt.wantPools {
				t.Errorf("got %d pools, want %d", len(pools), tt.wantPools)
			}
		})
	}
}

// A malformed device table makes the parser panic internally; the recover
// handler must turn that into an error.
func TestParseZpoolStatusPanicRecovered(t *testing.T) {
	tests := []struct {
		name string
		conf string
	}{
		{"whitespace-only line", "config:\n\n\tNAME STATE READ WRITE CKSUM\n\t   \n\nerrors: No known data errors\n"},
		{"header cut short", "config:\n\n\tNAME\n\nerrors: No known data errors\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pools, err := parseZpoolStatus("  pool: tank\n state: ONLINE\n" + tt.conf)
			if err == nil {
				t.Fatalf("expected an error, got pools=%v", pools)
			}
		})
	}
}

func TestParseZpoolStatusInlineConfig(t *testing.T) {
	p := parseSingle(t, "  pool: tank\n state: ONLINE\n"+
		"config: tank ONLINE 0 0 0\n\tsda ONLINE 0 0 0\n\nerrors: No known data errors\n")
	if len(p.devs) != 2 || p.devs[0].name != "tank" {
		t.Errorf("unexpected devs: %+v", p.devs)
	}
}

func TestParseZpoolStatusScanContinuationSpaces(t *testing.T) {
	p := parseSingle(t, synthHeader+
		"  scan: scrub in progress since Sat Nov 21 16:00:00 2026\n"+
		"    1.00G / 2.00G scanned\n"+
		"    0B repaired, 50.00% done\n"+
		synthConfig)
	want := "scrub in progress since Sat Nov 21 16:00:00 2026\n1.00G / 2.00G scanned\n0B repaired, 50.00% done"
	if p.scan != want {
		t.Errorf("scan = %q, want %q", p.scan, want)
	}
}

func TestParseZpoolStatusTwoExtraSections(t *testing.T) {
	p := parseSingle(t, synthHeader+
		"  scan: none requested\n"+
		"checkpoint: created Sat Nov 21 16:00:00 2026, consumes 1.00M\n"+
		"expand: expansion of raidz1-0 in progress\n"+
		"\t1.00G copied\n"+
		synthConfig)
	if !strings.Contains(p.extra, "checkpoint: created") || !strings.Contains(p.extra, "\nexpand: expansion") ||
		!strings.Contains(p.extra, "\n1.00G copied") {
		t.Errorf("unexpected extra: %q", p.extra)
	}
}

// The OpenZFS source prints "The configuration cannot be determined." as the
// only line of the config section (with a leading tab).
func TestParseZpoolStatusConfigUndetermined(t *testing.T) {
	p := parseSingle(t, "  pool: tank\n state: UNAVAIL\n"+
		"config:\n\n\tThe configuration cannot be determined.\n\nerrors: No known data errors\n")
	if len(p.devs) != 0 {
		t.Errorf("expected no devices, got devs=%+v", p.devs)
	}
}

func TestParseConfstr(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		devs, err := parseConfstr("")
		if err != nil || len(devs) != 0 {
			t.Errorf("got %v, %v", devs, err)
		}
	})
	t.Run("only blank lines", func(t *testing.T) {
		devs, err := parseConfstr("\n\n")
		if err != nil || len(devs) != 0 {
			t.Errorf("got %v, %v", devs, err)
		}
	})
	t.Run("broken numbers become -1", func(t *testing.T) {
		devs, err := parseConfstr("tank ONLINE x y -\n")
		if err != nil || len(devs) != 1 {
			t.Fatalf("got %v, %v", devs, err)
		}
		d := devs[0]
		if d.read != -1 || d.write != -1 || d.cksum != -1 {
			t.Errorf("unexpected counters: %+v", d)
		}
	})
	t.Run("class sections without state", func(t *testing.T) {
		devs, err := parseConfstr("tank ONLINE 0 0 0\n  logs\n    sdc ONLINE 0 0 0\n  cache\n    sdd ONLINE 0 0 0\n")
		if err != nil || len(devs) != 5 {
			t.Fatalf("got %v, %v", devs, err)
		}
		logs := devs[1]
		if logs.name != "logs" || logs.state != "" || logs.read != -1 || len(logs.subDevs) != 1 {
			t.Errorf("unexpected logs entry: %+v", logs)
		}
		if devs[4].parentDev != 3 {
			t.Errorf("sdd parent = %d, want 3", devs[4].parentDev)
		}
	})
	t.Run("dedent by several levels", func(t *testing.T) {
		devs, err := parseConfstr("tank ONLINE 0 0 0\n  mirror-0 ONLINE 0 0 0\n    sda ONLINE 0 0 0\n  mirror-1 ONLINE 0 0 0\n")
		if err != nil || len(devs) != 4 || devs[3].parentDev != 0 {
			t.Errorf("got %+v, %v", devs, err)
		}
	})
	t.Run("rest keeps extra columns", func(t *testing.T) {
		devs, _ := parseConfstr("sda FAULTED 1 2 3 too many errors\n")
		if len(devs) != 1 || devs[0].rest != "too many errors" || devs[0].state != "FAULTED" {
			t.Errorf("unexpected: %+v", devs)
		}
	})
}
