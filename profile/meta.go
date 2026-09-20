package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/zamiba/go-mediaitems/jsonfile"
)

// metadata is the content of profile.json.
//
// Only the fields this package understands are typed; everything else in the
// file rides along in obj, in its original order, so a program that does not
// know about a key another program wrote preserves it rather than deleting it
// on the next rename. The file is meant to be shared across the suite and
// written by hand if someone wants to, so a rename must not rewrite what it did
// not change. How the bytes are read and written is jsonfile's job.
type metadata struct {
	name      string
	createdAt time.Time
	createdBy string
	obj       jsonfile.Object // the file as read, minus the typed keys
}

// The keys this package owns. Three are meta fields - they describe the file
// rather than the person - and carry the standard's "_" prefix for exactly the
// reason the standard gives it: so they are distinguishable at a glance from
// data fields, of which there is one, name.
const (
	keySchemaVersion = "_schemaVersion"
	keyCreatedAt     = "_createdAt"
	keyCreatedBy     = "_createdBy"
	keyName          = "name"
)

// canonicalOrder is the order a new file gets. An existing file keeps its own.
var canonicalOrder = []string{keySchemaVersion, keyCreatedAt, keyCreatedBy, keyName}

func readMeta(path string) (metadata, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return metadata{}, err // fs.ErrNotExist passes through for the caller
	}
	obj, err := jsonfile.Decode(body)
	if err != nil {
		return metadata{}, fmt.Errorf("profile: %s: %w", path, err)
	}
	var m metadata
	if v, ok := obj.Values[keyName]; ok {
		if err := json.Unmarshal(v, &m.name); err != nil {
			return metadata{}, fmt.Errorf("profile: %s: %s: %w", path, keyName, err)
		}
		delete(obj.Values, keyName)
	}
	if v, ok := obj.Values[keyCreatedBy]; ok {
		if err := json.Unmarshal(v, &m.createdBy); err != nil {
			return metadata{}, fmt.Errorf("profile: %s: %s: %w", path, keyCreatedBy, err)
		}
		delete(obj.Values, keyCreatedBy)
	}
	if v, ok := obj.Values[keyCreatedAt]; ok {
		// A timestamp this package cannot read is still somebody's data. Left
		// in obj, it is written back untouched instead of silently deleted,
		// which is the rule for every other key we do not understand.
		var str string
		if err := json.Unmarshal(v, &str); err == nil {
			if t, err := time.Parse(time.RFC3339, str); err == nil {
				m.createdAt = t
				delete(obj.Values, keyCreatedAt)
			}
		}
	}
	delete(obj.Values, keySchemaVersion)
	m.obj = obj
	if m.name == "" {
		return metadata{}, fmt.Errorf("profile: %s: no name", path)
	}
	return m, nil
}

// writeMeta writes profile.json. With exclusive set the file must not exist
// yet - that is how Create detects a profile already there - and the error is
// fs.ErrExist. Otherwise the file is replaced atomically, so a reader never
// sees a half-written one.
func writeMeta(path string, m metadata, exclusive bool) error {
	body, err := m.marshal()
	if err != nil {
		return err
	}
	if exclusive {
		return jsonfile.CreateExclusive(path, body)
	}
	if err := jsonfile.WriteAtomic(path, body); err != nil {
		return fmt.Errorf("profile: %w", err)
	}
	return nil
}

// marshal renders profile.json: the file's own key order, unknown keys
// preserved, our keys set last so a stray copy in the file cannot displace
// them, byte-stable output.
func (m metadata) marshal() ([]byte, error) {
	obj := m.obj.Clone()
	version, err := json.Marshal(SchemaVersion)
	if err != nil {
		return nil, err
	}
	obj.Set(keySchemaVersion, version)
	if err := obj.SetString(keyName, m.name); err != nil {
		return nil, err
	}
	if !m.createdAt.IsZero() {
		if err := obj.SetString(keyCreatedAt, m.createdAt.UTC().Format(time.RFC3339)); err != nil {
			return nil, err
		}
	}
	if m.createdBy != "" {
		if err := obj.SetString(keyCreatedBy, m.createdBy); err != nil {
			return nil, err
		}
	}
	out, err := obj.Encode(canonicalOrder...)
	if err != nil {
		return nil, fmt.Errorf("profile: %w", err)
	}
	return out, nil
}
