package profile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zamiba/go-mediaitems/itemtitle"
	"github.com/zamiba/go-mediaitems/storageunit"
)

// Options configures a Manager. Dir is the profiles folder; empty means
// DefaultDir(). A program sets it for tests and for nothing else - every
// suite program on a device must read the same folder, or a profile created
// in one is invisible to the rest.
type Options struct {
	Dir string
}

// Manager reads and writes the profiles folder. It holds no copy of what is
// in it: every call reads the disk, so a profile another program created a
// moment ago is already there.
type Manager struct {
	dir string
}

// DefaultDir returns the shared profiles folder, storageunit.DefaultDir()/
// profiles: ${XDG_CONFIG_HOME:-~/.config}/MediaItem/profiles on Linux, and
// the equivalent beside storage-units.json on macOS and Windows. Resolving it
// through the storageunit package is what keeps the two in the same place.
func DefaultDir() (string, error) {
	base, err := storageunit.DefaultDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, DirName), nil
}

// New returns a Manager for the folder in opts, creating the folder if it is
// not there.
func New(opts Options) (*Manager, error) {
	dir := opts.Dir
	if dir == "" {
		d, err := DefaultDir()
		if err != nil {
			return nil, err
		}
		dir = d
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("profile: resolving %s: %w", dir, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("profile: creating %s: %w", abs, err)
	}
	return &Manager{dir: abs}, nil
}

// Open returns a Manager for the default folder.
func Open() (*Manager, error) { return New(Options{}) }

// Dir returns the profiles folder.
func (m *Manager) Dir() string { return m.dir }

// List returns every profile in the folder, by slug. A folder without a
// profile.json is not a profile and is skipped - a stray directory, or a
// profile mid-copy from another device - and a profile.json that will not
// parse is skipped too, rather than failing the whole list for one bad file.
func (m *Manager) List() ([]Profile, error) {
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return nil, fmt.Errorf("profile: reading %s: %w", m.dir, err)
	}
	var out []Profile
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p, err := m.Get(e.Name())
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out, nil
}

// Get returns the profile with the given slug.
func (m *Manager) Get(slug string) (Profile, error) {
	dir, err := m.profileDir(slug)
	if err != nil {
		return Profile{}, err
	}
	meta, err := readMeta(filepath.Join(dir, FileName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Profile{}, ErrNotFound
		}
		return Profile{}, err
	}
	return Profile{
		Slug:      slug,
		Name:      meta.name,
		CreatedAt: meta.createdAt,
		CreatedBy: meta.createdBy,
		Path:      dir,
		Picture:   picturePath(dir),
	}, nil
}

// Create makes a new profile named name, in the folder Slugify(name).
// createdBy is the program doing it. A profile that is already there is
// ErrExists, never overwritten: profile.json is the only file Create writes,
// and it is created exclusively, so two programs creating the same profile at
// once cannot both think they made it.
func (m *Manager) Create(name, createdBy string) (Profile, error) {
	name = strings.TrimSpace(name)
	slug := Slugify(name)
	if slug == "" {
		return Profile{}, ErrEmptyName
	}
	// Refused here rather than at mkdir, because on Linux the folder is made
	// happily and the profile only breaks later, on whichever device the
	// person copies it to. Get and List do not apply this: a profile that
	// already exists must stay reachable, and refusing to find one would lose
	// somebody's data rather than protect it.
	if itemtitle.Reserved(slug) {
		return Profile{}, fmt.Errorf("%w: %q", ErrReservedName, slug)
	}
	dir := filepath.Join(m.dir, slug)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Profile{}, fmt.Errorf("profile: creating %s: %w", dir, err)
	}
	meta := metadata{
		name:      name,
		createdAt: time.Now().UTC().Truncate(time.Second),
		createdBy: createdBy,
	}
	if err := writeMeta(filepath.Join(dir, FileName), meta, true); err != nil {
		if errors.Is(err, fs.ErrExist) {
			return Profile{}, ErrExists
		}
		return Profile{}, err
	}
	return Profile{Slug: slug, Name: name, CreatedAt: meta.createdAt, CreatedBy: createdBy, Path: dir}, nil
}

// Ensure returns the profile named name, creating it if it is not there. It
// is how a program gets its default profile - the one it uses for a person
// who did not ask for profiles - without caring whether this is the first run.
func (m *Manager) Ensure(name, createdBy string) (Profile, error) {
	p, err := m.Create(name, createdBy)
	if errors.Is(err, ErrExists) {
		return m.Get(Slugify(name))
	}
	return p, err
}

// Rename changes a profile's display name. The folder keeps its slug: the
// slug is the profile's identity, and every path and link into the folder -
// from every program on the device - would break with it.
func (m *Manager) Rename(slug, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrEmptyName
	}
	dir, err := m.profileDir(slug)
	if err != nil {
		return err
	}
	file := filepath.Join(dir, FileName)
	meta, err := readMeta(file)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	meta.name = name
	return writeMeta(file, meta, false)
}

// Delete removes a profile - the folder and everything in it, including a
// .git inside it - from this device.
//
// This is the one destructive call in the package, and it is here rather than
// left to os.RemoveAll in each program because every program must delete the
// same way: if a profile ever gains an index entry or a lock, deletion has to
// know. What it does not do is decide. A profile is somebody's saves, so a
// program calling this must have made the person confirm it in terms that say
// what is lost - the module trusts the caller on that and does nothing to
// second-guess it.
//
// It removes only this device's copy. Copies on other devices, and anything a
// sync backend has already pushed, are untouched. It is not undoable from
// here. A slug with no profile is ErrNotFound; a stray folder without a
// profile.json is not a profile and cannot be deleted through this call.
func (m *Manager) Delete(slug string) error {
	p, err := m.Get(slug)
	if err != nil {
		return err
	}
	if err := removeAll(p.Path); err != nil {
		return fmt.Errorf("profile: deleting %s: %w", p.Path, err)
	}
	return nil
}

// removeAll is os.RemoveAll with one retry after making the tree writable.
// A git repository marks its object files read-only, and on Windows that is
// enough to make removal fail with "access denied"; on Unix it is the parent
// directory's permission that matters, so the retry is a no-op there.
func removeAll(dir string) error {
	err := os.RemoveAll(dir)
	if err == nil {
		return nil
	}
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		mode := fs.FileMode(0o600)
		if d.IsDir() {
			mode = 0o700
		}
		_ = os.Chmod(path, mode)
		return nil
	})
	return os.RemoveAll(dir)
}

// profileDir resolves a slug to its folder, refusing anything that could reach
// past the profiles folder.
//
// itemtitle.PathSafe, not ValidFolderName: this is the one place in the suite
// that deliberately takes the security floor alone. A slug is not an
// _itemTitle - it is deliberately reductive, and it is an identity that
// already exists in folders on people's machines. Holding it to the standard's
// rule would not prevent a bad profile from being made; it would make an
// existing profile unreachable, which is much the worse failure. Create is
// where a new slug is judged, and it is stricter.
func (m *Manager) profileDir(slug string) (string, error) {
	if !itemtitle.PathSafe(slug) {
		return "", ErrNotFound
	}
	return filepath.Join(m.dir, slug), nil
}
