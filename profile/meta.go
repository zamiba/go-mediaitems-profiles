package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
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
	schema    string          // the file's declared _schemaVersion, "" if unreadable
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
	if v, ok := obj.Values[keySchemaVersion]; ok {
		m.schema = readSchemaVersion(v)
		delete(obj.Values, keySchemaVersion)
	}
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
	if err := obj.SetString(keySchemaVersion, m.writtenSchemaVersion()); err != nil {
		return nil, err
	}
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

// readSchemaVersion interprets the _schemaVersion a file declares, in either
// form it can legitimately be in: the MAJOR.MINOR string this package writes
// now, or the bare number it wrote before 2026-09-28, which means that major
// version and minor 0. Anything else - free text, a version with a part that
// is not a plain integer - reads as "" and is treated as undeclared.
//
// A version is not user data the way a name or a timestamp is; it is this
// package's own marker for how to read the rest of the file. So an unreadable
// one is replaced rather than preserved, which is the opposite of the rule for
// _createdAt, and deliberately so: keeping a version nobody can compare would
// mean carrying a claim about the file's shape that no code can act on.
func readSchemaVersion(raw json.RawMessage) string {
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		if _, _, ok := parseSchemaVersion(str); ok {
			return str
		}
		return ""
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil && n >= 0 {
		return strconv.Itoa(n) + ".0"
	}
	return ""
}

// writtenSchemaVersion is the version marshal stamps on the file: ours, unless
// the file already declared a newer one.
//
// The guard matters because this package rewrites whole files - a rename
// touches every key - so without it, an old build opening a profile written by
// a future one would quietly relabel it as older than it is while faithfully
// preserving the fields that made it newer. That is worse than either half
// alone: the file would keep its new shape and lose the only marker saying so.
func (m metadata) writtenSchemaVersion() string {
	if newerSchemaVersion(m.schema, SchemaVersion) {
		return m.schema
	}
	return SchemaVersion
}

// newerSchemaVersion reports whether a is a later version than b. An
// unparseable version is never later than anything.
func newerSchemaVersion(a, b string) bool {
	aMajor, aMinor, ok := parseSchemaVersion(a)
	if !ok {
		return false
	}
	bMajor, bMinor, ok := parseSchemaVersion(b)
	if !ok {
		return true
	}
	if aMajor != bMajor {
		return aMajor > bMajor
	}
	return aMinor > bMinor
}

// parseSchemaVersion splits MAJOR.MINOR. Both parts must be plain digits: a
// version string is not a decimal number, because "1.10" is newer than "1.9"
// and no numeric parse would tell you that.
func parseSchemaVersion(s string) (major, minor int, ok bool) {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0, 0, false
	}
	if major, ok = digits(s[:dot]); !ok {
		return 0, 0, false
	}
	if minor, ok = digits(s[dot+1:]); !ok {
		return 0, 0, false
	}
	return major, minor, true
}

// digits parses an unsigned decimal integer, rejecting the signs and spaces
// strconv.Atoi would otherwise accept.
func digits(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}
