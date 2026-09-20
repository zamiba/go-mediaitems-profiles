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

// profileDir resolves a slug to its folder, refusing anything that is not a
// plain folder name so a slug from outside cannot reach past the profiles
// folder.
func (m *Manager) profileDir(slug string) (string, error) {
	if !isFolderName(slug) {
		return "", ErrNotFound
	}
	return filepath.Join(m.dir, slug), nil
}
