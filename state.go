package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/tantefred/zfswatcher/notifier"
)

// Persistence of the pool state over daemon restarts. The file holds exactly
// the data which checkZpoolStatus and checkZfsUsage compare, so that after a
// restart only changes since the last run are reported. It uses its own
// exported types instead of the internal parser types.

const (
	stateFileVersion = 1
	stateFileMode    = 0o640

	// defaultStatefile is used if the configuration does not set "statefile".
	defaultStatefile = "/var/lib/zfswatcher/state.json"
)

type stateDev struct {
	Name  string `json:"name"`
	State string `json:"state"`
	Read  int64  `json:"read"`
	Write int64  `json:"write"`
	Cksum int64  `json:"cksum"`
	Rest  string `json:"rest,omitempty"`
	// Virtual is true for intermediary devices (mirror-N, raidz-N, class
	// sections) which have sub devices; the comparison skips them.
	Virtual bool `json:"virtual,omitempty"`
}

type statePool struct {
	Name   string     `json:"name"`
	State  string     `json:"state"`
	Status string     `json:"status,omitempty"`
	Errors string     `json:"errors,omitempty"`
	Devs   []stateDev `json:"devs,omitempty"`
}

type stateUsage struct {
	Avail int64 `json:"avail"`
	Used  int64 `json:"used"`
}

type stateFile struct {
	Version int                   `json:"version"`
	Pools   []statePool           `json:"pools"`
	Usage   map[string]stateUsage `json:"usage,omitempty"`
}

// newStateFile builds the persistent snapshot from the parsed state.
func newStateFile(pools []*PoolType, usage map[string]*PoolUsageType) *stateFile {
	sf := &stateFile{Version: stateFileVersion, Pools: []statePool{}}
	for _, p := range pools {
		sp := statePool{Name: p.name, State: p.state, Status: p.status, Errors: p.errors}
		for _, d := range p.devs {
			sp.Devs = append(sp.Devs, stateDev{
				Name: d.name, State: d.state, Read: d.read, Write: d.write,
				Cksum: d.cksum, Rest: d.rest, Virtual: len(d.subDevs) != 0,
			})
		}
		sf.Pools = append(sf.Pools, sp)
	}
	if len(usage) > 0 {
		sf.Usage = map[string]stateUsage{}
		for name, u := range usage {
			sf.Usage[name] = stateUsage{Avail: u.Avail, Used: u.Used}
		}
	}
	return sf
}

// poolTypes converts the snapshot back into the types used for comparison.
// Only the fields compared by checkZpoolStatus are filled in; the device
// tree links are not restored (a virtual device gets a dummy sub device
// entry because the comparison only looks at len(subDevs)).
func (sf *stateFile) poolTypes() []*PoolType {
	var pools []*PoolType
	for _, sp := range sf.Pools {
		p := &PoolType{name: sp.Name, state: sp.State, status: sp.Status, errors: sp.Errors}
		for _, sd := range sp.Devs {
			d := &DevEntry{name: sd.Name, state: sd.State, read: sd.Read,
				write: sd.Write, cksum: sd.Cksum, rest: sd.Rest}
			if sd.Virtual {
				d.subDevs = []int{0}
			}
			p.devs = append(p.devs, d)
		}
		pools = append(pools, p)
	}
	return pools
}

func (sf *stateFile) usageTypes() map[string]*PoolUsageType {
	usage := map[string]*PoolUsageType{}
	for name, u := range sf.Usage {
		usage[name] = &PoolUsageType{Name: name, Avail: u.Avail, Used: u.Used}
	}
	return usage
}

// usagePercents is what decides whether the usage part changed; raw byte
// counts change all the time and must not trigger a write each time.
func (sf *stateFile) usagePercents() map[string]int {
	m := map[string]int{}
	for name, u := range sf.Usage {
		if u.Avail+u.Used > 0 {
			m[name] = (&PoolUsageType{Avail: u.Avail, Used: u.Used}).GetUsedPercent()
		} else {
			m[name] = 0
		}
	}
	return m
}

// sameAs reports whether writing other would not tell anything new.
func (sf *stateFile) sameAs(other *stateFile) bool {
	return reflect.DeepEqual(sf.Pools, other.Pools) &&
		reflect.DeepEqual(sf.usagePercents(), other.usagePercents())
}

// loadStateFile reads a state file. A missing file is reported with an error
// for which errors.Is(err, fs.ErrNotExist) holds.
func loadStateFile(path string) (*stateFile, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path comes from the configuration
	if err != nil {
		return nil, err
	}
	var sf stateFile
	if err := json.Unmarshal(data, &sf); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w", err)
	}
	if sf.Version != stateFileVersion {
		return nil, fmt.Errorf("unsupported format version %d (expected %d)", sf.Version, stateFileVersion)
	}
	return &sf, nil
}

// writeStateFile writes atomically: temporary file in the same directory,
// fsync, rename.
func writeStateFile(path string, sf *stateFile) (err error) {
	data, err := json.MarshalIndent(sf, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(stateFileMode); err != nil {
		return err
	}
	if _, err = tmp.Write(data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	// Make the rename durable; not all platforms can sync a directory.
	if d, derr := os.Open(dir); derr == nil { //nolint:gosec // directory of the configured file
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// statePersister keeps the state file up to date. A nil *statePersister (no
// "statefile" configured) does nothing.
type statePersister struct {
	path    string
	last    *stateFile
	failing bool // last write failed; avoids logging the same error each cycle
}

// newStatePersister returns nil for an empty path (persistence disabled).
func newStatePersister(path string) *statePersister {
	if path == "" {
		return nil
	}
	return &statePersister{path: path}
}

// loadPrevious reads the state file at startup and logs what happened. It
// returns nil if there is no usable previous state.
func (sp *statePersister) loadPrevious() *stateFile {
	if sp == nil {
		return nil
	}
	sf, err := loadStateFile(sp.path)
	switch {
	case err == nil:
		sp.last = sf
		notify.Printf(notifier.INFO, "loaded previous state from %s", sp.path)
		return sf
	case errors.Is(err, fs.ErrNotExist):
		notify.Printf(notifier.INFO, "no state file %s yet, reporting the current state as new", sp.path)
	default:
		notify.Printf(notifier.WARNING, "ignoring state file %s: %s", sp.path, err)
	}
	return nil
}

// update writes the state file if the state differs from what was written
// (or loaded) last.
func (sp *statePersister) update(pools []*PoolType, usage map[string]*PoolUsageType) {
	if sp == nil {
		return
	}
	sf := newStateFile(pools, usage)
	if sp.last != nil && sf.sameAs(sp.last) {
		return
	}
	if err := writeStateFile(sp.path, sf); err != nil {
		if !sp.failing {
			notify.Printf(notifier.ERR, "cannot write state file %s: %s", sp.path, err)
		}
		sp.failing = true
		return
	}
	if sp.failing {
		notify.Printf(notifier.INFO, "state file %s is writable again", sp.path)
	}
	sp.failing = false
	sp.last = sf
}
