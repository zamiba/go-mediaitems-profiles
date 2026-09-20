package profilesync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

// FileName is the device-local sync configuration, beside the profiles folder:
// <config>/MediaItem/profile-sync.json.
//
// It is outside every profile on purpose. A git remote, an rclone target or a
// Syncthing API key is only true on the device it was configured on, and a
// profile is meant to be copied between devices intact. Anything in here would
// travel with the profile and be wrong on arrival.
const FileName = "profile-sync.json"

// Kinds of backend. Two rclone kinds rather than one with a switch, because the
// difference between them is whether files can be deleted from the remote, and
// a choice like that belongs in the name a person reads in the config file, not
// in a boolean beside it.
const (
	KindGit        = "git"
	KindSyncthing  = "syncthing"
	KindRcloneCopy = "rclone-copy" // rclone copy: never deletes on the remote
	KindRcloneSync = "rclone-sync" // rclone sync: a mirror; deletes remote files absent locally
)

var kinds = map[string]bool{KindGit: true, KindSyncthing: true, KindRcloneCopy: true, KindRcloneSync: true}

// Config is the parsed profile-sync.json.
//
// It is read, never written, by this package - a person or a settings screen
// writes it - so it is decoded straight into structs and keys this package does
// not know are ignored rather than preserved.
type Config struct {
	Version int `json:"version"`

	// Profiles maps a slug, or "*" for every profile, to the backends that run
	// when it changes. A slug's own entries run after the "*" entries.
	Profiles map[string][]Backend `json:"profiles"`
}

// Backend is one place a changed profile is sent.
type Backend struct {
	// Kind is one of the Kind constants.
	Kind string `json:"kind"`

	// Remote is the git remote name to push to, or the rclone target as
	// "remote:path". For git it is optional: with no remote the profile is
	// committed and not pushed.
	Remote string `json:"remote,omitempty"`

	// Syncthing only: the REST API base URL (default http://127.0.0.1:8384),
	// the API key, and the id of the Syncthing folder holding the profile.
	URL      string `json:"url,omitempty"`
	APIKey   string `json:"apiKey,omitempty"`
	FolderID string `json:"folderId,omitempty"`
}

// loadConfig reads the file. A missing file is an empty configuration, not an
// error: implicit git still works with no file at all.
func loadConfig(path string) (Config, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("profilesync: reading %s: %w", path, err)
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return Config{}, fmt.Errorf("profilesync: %s is not valid JSON: %w", path, err)
	}
	for slug, backends := range cfg.Profiles {
		for i, b := range backends {
			if err := b.validate(); err != nil {
				return Config{}, fmt.Errorf("profilesync: %s: profiles[%q][%d]: %w", path, slug, i, err)
			}
		}
	}
	return cfg, nil
}

func (b Backend) validate() error {
	if !kinds[b.Kind] {
		valid := []string{KindGit, KindSyncthing, KindRcloneCopy, KindRcloneSync}
		return fmt.Errorf("unknown kind %q (valid: %s)", b.Kind, strings.Join(valid, ", "))
	}
	switch b.Kind {
	case KindRcloneCopy, KindRcloneSync:
		if b.Remote == "" {
			return fmt.Errorf("%s needs a remote such as \"backup:profiles/sam\"", b.Kind)
		}
	case KindSyncthing:
		if b.FolderID == "" {
			return errors.New("syncthing needs the folderId of the Syncthing folder holding the profile")
		}
	}
	return nil
}

// backendsFor returns the backends that apply to a slug: the "*" entries, then
// the slug's own.
func (c Config) backendsFor(slug string) []Backend {
	var out []Backend
	out = append(out, c.Profiles["*"]...)
	out = append(out, c.Profiles[slug]...)
	return out
}
