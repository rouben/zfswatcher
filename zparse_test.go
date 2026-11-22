package main

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/tantefred/zfswatcher/notifier"
)

func TestMain(m *testing.M) {
	// The parsers report problems through the global notifier.
	notify = notifier.New()
	os.Exit(m.Run())
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("test/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestUnniceNumber(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"0B", 0},
		{"512B", 512},
		{"42", 42},
		{"1K", 1024},
		{"1.5K", 1536},
		{"100M", 100 * 1024 * 1024},
		{"1.5G", 3 << 29},
		{"2T", 2 << 40},
		{"-", -1},
		{"garbage", -1},
	}
	for _, tt := range tests {
		if got := unniceNumber(tt.in); got != tt.want {
			t.Errorf("unniceNumber(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestPoolUsagePercent(t *testing.T) {
	u := &PoolUsageType{Avail: 75, Used: 25}
	if got := u.GetUsedPercent(); got != 25 {
		t.Errorf("GetUsedPercent() = %d, want 25", got)
	}
	if got := u.GetAvailPercent(); got != 75 {
		t.Errorf("GetAvailPercent() = %d, want 75", got)
	}
}

func TestParseZfsList(t *testing.T) {
	usage := parseZfsList(readFixture(t, "zfs-list.txt"))
	if len(usage) != 2 {
		t.Fatalf("got %d entries, want 2", len(usage))
	}
	tank := usage["tank"]
	if tank == nil {
		t.Fatal("pool tank not found")
	}
	if tank.Mountpoint != "/tank" || tank.Compressratio != "1.05x" {
		t.Errorf("unexpected tank entry: %+v", tank)
	}
	if want := unniceNumber("155G"); tank.Avail != want {
		t.Errorf("tank.Avail = %d, want %d", tank.Avail, want)
	}
	if want := unniceNumber("380G"); tank.Used != want {
		t.Errorf("tank.Used = %d, want %d", tank.Used, want)
	}
}

func TestParseZfsListDetails(t *testing.T) {
	usage := parseZfsList(readFixture(t, "zfs-list-details.txt"))
	for _, name := range []string{"tank", "tank@snaptest", "tank/test-8k", "tank/xfs", "tank/xfs@backup"} {
		if usage[name] == nil {
			t.Errorf("entry %q not found", name)
		}
	}
	if got := usage["tank@snaptest"].Avail; got != -1 {
		t.Errorf("snapshot Avail = %d, want -1", got)
	}
}

func TestParseZfsListInvalidLine(t *testing.T) {
	usage := parseZfsList("tank\t1\t2\n")
	if len(usage) != 0 {
		t.Errorf("expected invalid line to be skipped, got %v", usage)
	}
}

func TestParseZfsListLegacy9Columns(t *testing.T) {
	legacyZfsListWarned.Store(false)
	in := "zfswt_a\t9.68M\t150M\t0\t150M\t0\t0\t150M\t/zfswt_a\n" +
		"zfswt_b\t1.5G\t2G\t0\t2G\t0\t0\t2G\t-\n"
	usage := parseZfsList(in)
	if len(usage) != 2 {
		t.Fatalf("got %d entries, want 2: %v", len(usage), usage)
	}
	u := usage["zfswt_a"]
	if u == nil {
		t.Fatal("zfswt_a missing")
	}
	if u.Avail != unniceNumber("9.68M") || u.Used != unniceNumber("150M") ||
		u.Usedds != unniceNumber("150M") || u.Refer != unniceNumber("150M") ||
		u.Mountpoint != "/zfswt_a" {
		t.Errorf("unexpected fields: %+v", u)
	}
	if u.Logicalused != -1 || u.Compressratio != "-" {
		t.Errorf("logicalused/compressratio = %d/%q, want -1/\"-\"", u.Logicalused, u.Compressratio)
	}
	if got := niceNumber(u.Logicalused); got != "-" {
		t.Errorf("niceNumber(unknown) = %q, want \"-\"", got)
	}
	if !legacyZfsListWarned.Load() {
		t.Error("legacy warning flag not set")
	}
	// A second parse must not trigger the warning again.
	if legacyZfsListWarned.CompareAndSwap(false, true) {
		t.Error("warning would be logged again")
	}
}

func TestParseZfsList11Columns(t *testing.T) {
	legacyZfsListWarned.Store(false)
	in := "zfswt_a\t9.68M\t150M\t160M\t0\t150M\t0\t0\t150M\t/zfswt_a\t1.07x\n"
	u := parseZfsList(in)["zfswt_a"]
	if u == nil {
		t.Fatal("zfswt_a missing")
	}
	if u.Logicalused != unniceNumber("160M") || u.Compressratio != "1.07x" {
		t.Errorf("unexpected fields: %+v", u)
	}
	if legacyZfsListWarned.Load() {
		t.Error("legacy warning set for 11 column input")
	}
}

func TestParseZfsListInvalid10Columns(t *testing.T) {
	in := "zfswt_a\t9.68M\t150M\t160M\t0\t150M\t0\t0\t150M\t/zfswt_a\n"
	if usage := parseZfsList(in); len(usage) != 0 {
		t.Errorf("expected 10 column line to be skipped, got %v", usage)
	}
}

func TestParseZpoolStatusDegraded(t *testing.T) {
	pools, err := parseZpoolStatus(readFixture(t, "zpool-status-degraded.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 {
		t.Fatalf("got %d pools, want 1", len(pools))
	}
	p := pools[0]
	if p.name != "test" || p.state != "DEGRADED" {
		t.Errorf("unexpected pool: name=%q state=%q", p.name, p.state)
	}
	if !strings.HasPrefix(p.status, "One or more devices could not be used") ||
		!strings.Contains(p.status, "invalid.  Sufficient replicas") {
		t.Errorf("unexpected status: %q", p.status)
	}
	if p.action != "Replace the device using 'zpool replace'." {
		t.Errorf("unexpected action: %q", p.action)
	}
	if p.see != "http://zfsonlinux.org/msg/ZFS-8000-4J" {
		t.Errorf("unexpected see: %q", p.see)
	}
	if !strings.HasPrefix(p.scan, "resilvered 674M") {
		t.Errorf("unexpected scan: %q", p.scan)
	}
	if p.errors != "No known data errors" {
		t.Errorf("unexpected errors: %q", p.errors)
	}

	byName := map[string]*DevEntry{}
	for _, d := range p.devs {
		byName[d.name] = d
	}
	if got := byName["B0"]; got == nil || got.state != "UNAVAIL" {
		t.Errorf("B0 = %+v, want state UNAVAIL", got)
	}
	if got := byName["raidz3-0"]; got == nil || got.state != "DEGRADED" || len(got.subDevs) != 7 {
		t.Errorf("raidz3-0 = %+v, want DEGRADED with 7 subdevices", got)
	}
	spares := byName["spares"]
	if spares == nil || len(spares.subDevs) != 2 || spares.parentDev != -1 {
		t.Errorf("spares = %+v, want root entry with 2 subdevices", spares)
	}
	if b1 := byName["B1"]; b1 == nil || b1.read != -1 {
		t.Errorf("B1 = %+v, want read counter -1", b1)
	}
}

func TestParseZpoolStatusTwoPools(t *testing.T) {
	pools, err := parseZpoolStatus(readFixture(t, "zpool-status-2pools.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 2 {
		t.Fatalf("got %d pools, want 2", len(pools))
	}
	if pools[0].name != "tank" || pools[1].name != "vmstore" {
		t.Errorf("pool names = %q, %q", pools[0].name, pools[1].name)
	}
	for _, p := range pools {
		if p.state != "ONLINE" || p.scan != "none requested" {
			t.Errorf("pool %s: state=%q scan=%q", p.name, p.state, p.scan)
		}
	}
	// tank: pool + 8 mirrors * 3 + logs + sdc
	if got := len(pools[0].devs); got != 1+8*3+2 {
		t.Errorf("tank has %d devs, want %d", got, 1+8*3+2)
	}
	if got := len(pools[1].devs); got != 2 {
		t.Errorf("vmstore has %d devs, want 2", got)
	}
}

func TestParseZpoolStatusNoPools(t *testing.T) {
	pools, err := parseZpoolStatus("no pools available\n")
	if err != nil || len(pools) != 0 {
		t.Errorf("got pools=%v err=%v, want none", pools, err)
	}
}

func TestParseZpoolStatusInvalid(t *testing.T) {
	if _, err := parseZpoolStatus("this is not zpool output\n"); err == nil {
		t.Error("expected an error for invalid input")
	}
}

func TestParseConfstrUndetermined(t *testing.T) {
	if _, err := parseConfstr("The configuration cannot be determined."); err == nil {
		t.Error("expected an error")
	}
}

func TestZpoolIostatParser(t *testing.T) {
	data := readFixture(t, "zpool-iostat.txt")
	first := data[:strings.Index(data, "\n\n")+1]

	table := ZpoolIostatParser(first)
	if table == nil {
		t.Fatal("parser returned nil")
	}
	tank := (*table)["tank"]
	if tank == nil {
		t.Fatal("pool tank not found")
	}
	row := tank["tank"]
	if row == nil || row.OperationsRead != 18 || row.OperationsWrite != 71 {
		t.Errorf("unexpected tank row: %+v", row)
	}
	if sdc := tank["sdc"]; sdc == nil || sdc.CapacityAlloc != 84*1024 {
		t.Errorf("unexpected sdc row: %+v", sdc)
	}
	if (*table)["vmstore"] == nil {
		t.Error("pool vmstore not found")
	}
}

func TestZpoolIostatParserInvalidRow(t *testing.T) {
	in := "header\n------  -----\ntank 1 2\n"
	if got := ZpoolIostatParser(in); got != nil {
		t.Errorf("expected nil for malformed row, got %v", got)
	}
}

func TestParseZpoolStatusProxmox(t *testing.T) {
	// Output of OpenZFS 2.x as shipped with Proxmox VE (three pools; device
	// serial numbers anonymized).
	pools, err := parseZpoolStatus(readFixture(t, "zpool-status-proxmox.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 3 {
		t.Fatalf("got %d pools, want 3", len(pools))
	}
	names := []string{"datis", "rpool", "secundus"}
	devCounts := []int{1 + 1 + 3, 1 + 1 + 2, 1 + 1 + 4}
	for i, p := range pools {
		if p.name != names[i] {
			t.Errorf("pool %d name = %q, want %q", i, p.name, names[i])
		}
		if p.state != "ONLINE" || p.errors != "No known data errors" {
			t.Errorf("pool %s: state=%q errors=%q", p.name, p.state, p.errors)
		}
		if !strings.HasPrefix(p.scan, "scrub repaired 0B in") {
			t.Errorf("pool %s: unexpected scan %q", p.name, p.scan)
		}
		if got := len(p.devs); got != devCounts[i] {
			t.Errorf("pool %s has %d devs, want %d", p.name, got, devCounts[i])
		}
	}

	datis := pools[0]
	if !strings.HasPrefix(datis.status, "Some supported and requested features are not enabled") ||
		!strings.Contains(datis.status, "\nThe pool can still be used") {
		t.Errorf("datis: unexpected status %q", datis.status)
	}
	if !strings.HasPrefix(datis.action, "Enable all features using 'zpool upgrade'.") ||
		!strings.HasSuffix(datis.action, "zpool-features(7) for details.") {
		t.Errorf("datis: unexpected action %q", datis.action)
	}
	if pools[1].status != "" || pools[1].action != "" {
		t.Errorf("rpool should have no status/action: %q / %q", pools[1].status, pools[1].action)
	}

	// device tree of the rpool mirror (long device names, partition suffix)
	rpool := pools[1].devs
	if rpool[1].name != "mirror-0" || len(rpool[1].subDevs) != 2 {
		t.Errorf("unexpected mirror: %+v", rpool[1])
	}
	if got := rpool[2].name; got != "ata-SanDisk_SSD_PLUS_120GB_201911A0000A-part3" {
		t.Errorf("unexpected device name %q", got)
	}
}

func TestParseZfsListProxmox(t *testing.T) {
	// OpenZFS 2.x prints zero sizes as "0B".
	usage := parseZfsList(readFixture(t, "zfs-list-proxmox.txt"))
	if len(usage) != 3 {
		t.Fatalf("got %d entries, want 3", len(usage))
	}
	rpool := usage["rpool"]
	if rpool == nil {
		t.Fatal("pool rpool not found")
	}
	if rpool.Usedsnap != 0 || rpool.Usedrefreserv != 0 {
		t.Errorf("0B must parse as 0, got usedsnap=%d usedrefreserv=%d", rpool.Usedsnap, rpool.Usedrefreserv)
	}
	if want := unniceNumber("53.5G"); rpool.Logicalused != want {
		t.Errorf("Logicalused = %d, want %d", rpool.Logicalused, want)
	}
	if rpool.Mountpoint != "/rpool" || rpool.Compressratio != "1.79x" {
		t.Errorf("unexpected rpool entry: %+v", rpool)
	}
	if got := rpool.GetUsedPercent(); got != 29 {
		t.Errorf("rpool used percent = %d, want 29", got)
	}
}

func TestZpoolIostatParserVerboseProxmox(t *testing.T) {
	table := ZpoolIostatParser(readFixture(t, "zpool-iostat-v-proxmox.txt"))
	if table == nil {
		t.Fatal("parser returned nil")
	}
	if len(*table) != 3 {
		t.Fatalf("got %d pools, want 3", len(*table))
	}
	rpool := (*table)["rpool"]
	if got := len(rpool); got != 4 { // pool, mirror and two devices
		t.Errorf("rpool has %d rows, want 4", got)
	}
	if row := rpool["sdi3"]; row == nil || row.OperationsRead != 24 || row.CapacityAlloc != -1 {
		t.Errorf("unexpected sdi3 row: %+v", row)
	}
	if row := (*table)["secundus"]["secundus"]; row == nil || row.CapacityFree != unniceNumber("14.4T") {
		t.Errorf("unexpected secundus row: %+v", row)
	}
	if got := len((*table)["secundus"]); got != 6 {
		t.Errorf("secundus has %d rows, want 6", got)
	}
}

func TestParseZfsListAllProxmox(t *testing.T) {
	// zfs list -r -t all: datasets, container volumes and snapshots.
	usage := parseZfsList(readFixture(t, "zfs-list-all-proxmox.txt"))
	if len(usage) != 31 {
		t.Fatalf("got %d entries, want 31", len(usage))
	}
	snaps := 0
	for name, u := range usage {
		if strings.Contains(name, "@") {
			snaps++
			if u.Avail != -1 || u.Logicalused != -1 || u.Mountpoint != "-" {
				t.Errorf("snapshot %s: unexpected entry %+v", name, u)
			}
		}
	}
	if snaps != 8 {
		t.Errorf("got %d snapshots, want 8", snaps)
	}

	vol := usage["rpool/data/subvol-100-disk-0"]
	if vol == nil {
		t.Fatal("subvol-100-disk-0 not found")
	}
	if want := unniceNumber("587M"); vol.Usedsnap != want {
		t.Errorf("Usedsnap = %d, want %d", vol.Usedsnap, want)
	}
	if usage["rpool/ROOT/pve-1"].Mountpoint != "/" {
		t.Errorf("root dataset mountpoint = %q", usage["rpool/ROOT/pve-1"].Mountpoint)
	}
	if got := usage["datis/Daten"].Mountpoint; got != "/mnt/Daten" {
		t.Errorf("datis/Daten mountpoint = %q", got)
	}
}

func TestZfsListErrorPaths(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int // number of entries in the result
	}{
		{"empty", "", 0},
		{"only newlines", "\n\n", 0},
		{"too few columns", "tank\t1\t2\n", 0},
		{"too many columns", strings.Repeat("a\t", 12) + "b\n", 0},
		{"broken numbers", "tank\tx\ty\tz\t1\t2\t3\t4\t5\t/tank\t1.00x\n", 1},
		{"one bad line among good", "tank\t1\t2\t3\t4\t5\t6\t7\t8\t/tank\t1.00x\nbad\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseZfsList(tt.in); len(got) != tt.want {
				t.Errorf("got %d entries, want %d", len(got), tt.want)
			}
		})
	}
	m := parseZfsList("tank\tx\ty\tz\t1\t2\t3\t4\t5\t/tank\t1.00x\n")
	if u := m["tank"]; u == nil || u.Avail != -1 || u.Used != -1 || u.Logicalused != -1 {
		t.Errorf("broken numbers should be -1: %+v", u)
	}
}

const (
	ioSep = "------  -----  -----  -----  -----  -----  -----\n"
	ioHdr = "pool  alloc  free  read  write  read  write\n"
)

func TestZpoolIostatParserEdgeCases(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		got := ZpoolIostatParser("")
		if got == nil || len(*got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("no separator", func(t *testing.T) {
		got := ZpoolIostatParser("tank 1 2 3 4 5 6\n")
		if got == nil || len(*got) != 0 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("consecutive separators", func(t *testing.T) {
		got := ZpoolIostatParser(ioHdr + ioSep + ioSep + "tank 1K 2K 3 4 5 6\n" + ioSep)
		if got == nil || (*got)["tank"] == nil || (*got)["tank"]["tank"].CapacityAlloc != 1024 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("malformed device row", func(t *testing.T) {
		if got := ZpoolIostatParser(ioHdr + ioSep + "tank 1 2 3 4 5 6\n  sda 1 2\n"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("row with too many columns", func(t *testing.T) {
		if got := ZpoolIostatParser(ioHdr + ioSep + "tank 1 2 3 4 5 6 7\n"); got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})
	t.Run("broken numbers", func(t *testing.T) {
		got := ZpoolIostatParser(ioHdr + ioSep + "tank a b c d e f\n")
		if got == nil || (*got)["tank"]["tank"].OperationsRead != -1 {
			t.Errorf("got %v", got)
		}
	})
	t.Run("stops at blank line", func(t *testing.T) {
		got := ZpoolIostatParser(ioHdr + ioSep + "tank 1 2 3 4 5 6\n\ngarbage\n")
		if got == nil || len(*got) != 1 {
			t.Errorf("got %v", got)
		}
	})
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestZpoolIostatStreamReader(t *testing.T) {
	entry := func(pool string) string {
		return ioHdr + ioSep + pool + " 1 2 3 4 5 6\n" + ioSep
	}
	collect := func(r io.Reader) []*ZpoolIostatTable {
		ch := make(chan *ZpoolIostatTable, 10)
		go ZpoolIostatStreamReader(ch, r)
		var out []*ZpoolIostatTable
		for tbl := range ch {
			out = append(out, tbl)
		}
		return out
	}
	t.Run("two entries", func(t *testing.T) {
		out := collect(iotest.OneByteReader(strings.NewReader(entry("a") + "\n" + entry("b") + "\n")))
		if len(out) != 2 || (*out[0])["a"] == nil || (*out[1])["b"] == nil {
			t.Errorf("got %v", out)
		}
	})
	t.Run("two entries in one read", func(t *testing.T) {
		out := collect(strings.NewReader(entry("a") + "\n" + entry("b") + "\n"))
		if len(out) != 2 {
			t.Errorf("got %d of 2 entries when both arrive in a single read", len(out))
		}
	})
	t.Run("truncated last entry is dropped", func(t *testing.T) {
		out := collect(strings.NewReader(entry("a") + "\n" + entry("b")))
		if len(out) != 1 {
			t.Errorf("got %d entries, want 1", len(out))
		}
	})
	t.Run("empty input", func(t *testing.T) {
		if out := collect(strings.NewReader("")); len(out) != 0 {
			t.Errorf("got %v", out)
		}
	})
	t.Run("read error", func(t *testing.T) {
		out := collect(io.MultiReader(strings.NewReader(entry("a")+"\n"), errReader{}))
		if len(out) != 2 || out[0] == nil || out[1] != nil {
			t.Errorf("expected a table and then nil, got %v", out)
		}
	})
	t.Run("malformed entry yields nil", func(t *testing.T) {
		out := collect(strings.NewReader(ioHdr + ioSep + "tank 1 2\n\n"))
		if len(out) != 1 || out[0] != nil {
			t.Errorf("got %v", out)
		}
	})
}
