package main

import (
	"strings"
	"testing"
)

// Fixtures in test/ named *-vdev-classes, *-spare-resilver etc. were captured
// with scripts/collect-zfs-fixtures.sh on Debian 13 (OpenZFS 2.3.9).
func TestParseZpoolStatusDebian(t *testing.T) {
	tests := []struct {
		file     string
		pool     string
		state    string
		scan     string // prefix
		extra    string // prefix
		numDevs  int
		devName  string // a device that must exist ...
		devState string // ... in this state
		devRest  string // ... with this comment
	}{
		{"zpool-status-offline.txt", "zfsw_mirror", "DEGRADED", "", "", 4,
			"/var/tmp/zfsw-collect/m2", "OFFLINE", ""},
		{"zpool-status-faulted.txt", "zfsw_mirror", "DEGRADED", "resilvered 18K", "", 4,
			"/var/tmp/zfsw-collect/m2", "FAULTED", "external device fault"},
		{"zpool-status-scrub-repairing.txt", "zfsw_mirror", "ONLINE", "scrub in progress", "", 4,
			"/var/tmp/zfsw-collect/m1", "ONLINE", "(repairing)"},
		{"zpool-status-scrub-repaired.txt", "zfsw_mirror", "ONLINE", "scrub repaired 7.08M", "", 4,
			"/var/tmp/zfsw-collect/m1", "ONLINE", ""},
		{"zpool-status-resilver-replacing.txt", "zfsw_mirror", "ONLINE", "resilver in progress", "", 6,
			"/var/tmp/zfsw-collect/m3", "ONLINE", "(resilvering)"},
		{"zpool-status-checkpoint.txt", "zfsw_mirror", "ONLINE", "resilvered 401M", "checkpoint: created", 6,
			"replacing-1", "ONLINE", ""},
		{"zpool-status-removal.txt", "zfsw_rem", "ONLINE", "", "remove: Evacuation", 7,
			"mirror-1", "ONLINE", "(removing)"},
		{"zpool-status-removal-done.txt", "zfsw_rem", "ONLINE", "", "remove: Removal of vdev 1", 4,
			"mirror-0", "ONLINE", ""},
		{"zpool-status-raidz-expand.txt", "zfsw_exp", "ONLINE", "", "expand: expansion of raidz1-0", 6,
			"raidz1-0", "ONLINE", ""},
		{"zpool-status-raidz-expand-scrub.txt", "zfsw_exp", "ONLINE", "scrub in progress", "expand: expanded raidz1-0", 6,
			"raidz1-0", "ONLINE", ""},
		{"zpool-status-vdev-classes.txt", "zfsw_mix", "ONLINE", "", "", 15,
			"/var/tmp/zfsw-collect/c6", "AVAIL", ""},
		{"zpool-status-spare-resilver.txt", "zfsw_mix", "DEGRADED", "resilver in progress", "", 17,
			"spare-1", "DEGRADED", ""},
		{"zpool-status-unavail.txt", "zfsw_un", "DEGRADED", "", "", 4,
			"14974271720844242149", "UNAVAIL", "was /var/tmp/zfsw-collect/u2"},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			pools, err := parseZpoolStatus(readFixture(t, tt.file))
			if err != nil {
				t.Fatal(err)
			}
			if len(pools) != 1 {
				t.Fatalf("got %d pools, want 1", len(pools))
			}
			p := pools[0]
			if p.name != tt.pool || p.state != tt.state {
				t.Errorf("pool %q state %q, want %q %q", p.name, p.state, tt.pool, tt.state)
			}
			if !strings.HasPrefix(p.scan, tt.scan) {
				t.Errorf("scan = %q, want prefix %q", p.scan, tt.scan)
			}
			if !strings.HasPrefix(p.extra, tt.extra) {
				t.Errorf("extra = %q, want prefix %q", p.extra, tt.extra)
			}
			if len(p.devs) != tt.numDevs {
				t.Errorf("got %d devices, want %d", len(p.devs), tt.numDevs)
			}
			found := false
			for _, d := range p.devs {
				if d.name == tt.devName {
					found = true
					if d.state != tt.devState || d.rest != tt.devRest {
						t.Errorf("%s: state %q rest %q, want %q %q",
							d.name, d.state, d.rest, tt.devState, tt.devRest)
					}
				}
			}
			if !found {
				t.Errorf("device %q not found", tt.devName)
			}
		})
	}
}

func TestParseZpoolStatusDebianCounters(t *testing.T) {
	pools, err := parseZpoolStatus(readFixture(t, "zpool-status-scrub-repaired.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range pools[0].devs {
		if d.name == "/var/tmp/zfsw-collect/m1" && d.cksum != 131 {
			t.Errorf("cksum = %d, want 131", d.cksum)
		}
	}
}

func TestZpoolIostatParserVdevClasses(t *testing.T) {
	table := ZpoolIostatParser(readFixture(t, "zpool-iostat-v-classes.txt"))
	if table == nil {
		t.Fatal("parse error")
	}
	rows := (*table)["zfsw_mix"]
	// pool, raidz1-0, 3 disks, 4 class headers and 4 class devices
	if len(rows) != 13 {
		t.Errorf("got %d rows, want 13", len(rows))
	}
	if r := rows["/var/tmp/zfsw-collect/c5"]; r == nil || r.CapacityAlloc != unniceNumber("69.5M") {
		t.Errorf("cache device row missing or wrong: %+v", r)
	}
}

func TestParseZfsListAllDebian(t *testing.T) {
	m := parseZfsList(readFixture(t, "zfs-list-all-debian.txt"))
	if len(m) != 9 {
		t.Fatalf("got %d entries, want 9", len(m))
	}
	if u := m["zfsw_mirror/vol"]; u == nil || u.Usedrefreserv != 67<<20 {
		t.Errorf("vol entry: %+v", u)
	}
	if u := m["zfsw_mirror/data@snap1"]; u == nil || u.Used != 0 {
		t.Errorf("snapshot entry: %+v", u)
	}
}
