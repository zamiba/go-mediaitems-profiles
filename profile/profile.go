// Package profile is the suite's shared notion of a person: a folder that holds
// everything the MediaItem programs record *about* someone - game saves,
// achievements, what they have watched - as opposed to the media itself, which
// lives on storage units.
//
// A profile is a plain directory. That is the whole design, and every rule
// below follows from it:
//
//   - **Filesystem-native, like MediaItems.** Nothing here is a database. A
//     profile can be copied, zipped, put in git, synced with Syncthing, or
//     opened in a file manager, and every program in the suite will read it.
//   - **Per device.** Each device has its own profiles folder and creates its
//     profiles locally. Keeping two devices' copies of a profile in step is
//     the job of whatever the user chooses - git, a sync tool, a copy - and
//     this package neither knows nor cares which. It never runs git.
//   - **Same layout as a storage unit.** Data about an item lives under
//     MediaItems/<ItemType>/<item title>/ inside the profile, mirroring where
//     the item itself lives on a storage unit, so a program that knows one
//     path knows the other. No program gets a namespace of its own: two
//     programs recording the same kind of fact - achievements, say - write
//     the same files, and agree on their shape.
//   - **Mergeable by design.** A profile from another device is reconciled
//     file by file, so programs should record facts as small files rather
//     than one big one, and never write absolute paths, lock files or
//     anything else that is only true on the device that wrote it.
//
// The folder name is the profile's identity. It is the slug of the name the
// profile was created with, and the same name on two devices gives the same
// slug, which is what lets the two be recognised as one profile when they
// meet. Renaming changes the display name only; the folder - and so every
// link and path that points into it - stays put.
package profile

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	// DirName is the profiles folder, under the suite's configuration folder
	// (storageunit.DefaultDir()). Profiles sit beside storage-units.json
	// rather than on a storage unit because a unit can be unplugged, and a
	// person's saves must not vanish with the drive their games are on.
	DirName = "profiles"

	// FileName is the metadata file at the root of every profile folder. Its
	// presence is what makes a folder a profile.
	FileName = "profile.json"

	// ItemsDirName is the folder inside a profile that mirrors a storage
	// unit's layout: ItemsDirName/<ItemType>/<item title>/.
	ItemsDirName = "MediaItems"

	// SchemaVersion is written to every profile.json as _schemaVersion.
	SchemaVersion = 1
)

// Profile is one person's folder, as seen by a caller.
//
// Name, CreatedAt and CreatedBy persist in profile.json. Slug is the folder
// name and Path is where the folder is on this device; neither is written to
// the file, so the file cannot disagree with the folder it is in.
type Profile struct {
	// Slug is the folder name and the profile's identity: the slug of the
	// name it was created with. It never changes.
	Slug string `json:"slug"`

	// Name is the display label, user-editable through Rename.
	Name string `json:"name"`

	// CreatedAt is when the profile was created, on whichever device did so.
	CreatedAt time.Time `json:"createdAt"`

	// CreatedBy names the program that created the profile - "portforge",
	// say. A program that makes a default profile for people who did not ask
	// for one records itself here, so another program can tell that profile
	// from one the person chose to create.
	CreatedBy string `json:"createdBy,omitempty"`

	// Path is the absolute path of the profile folder on this device.
	Path string `json:"path"`

	// Picture is the absolute path of the profile's picture on this device -
	// Path/picture.png, a PictureSize-square PNG - or empty if it has none.
	// A program with nothing to show falls back to the name's first letter.
	Picture string `json:"picture,omitempty"`
}

// ItemDir is where a program keeps what it records about one item for this
// profile: Path/MediaItems/<itemType>/<itemTitle>/. It mirrors the item's own
// folder on a storage unit, so the two are found by the same two names.
//
// The folder is not created; the caller does that when it has something to
// write, so listing a profile shows only items it holds data for.
//
// Both names must be single folder names. An item title comes from catalogue
// data - a .mediaitem.json somebody else wrote - and a title containing a
// separator or ".." would otherwise resolve to a path outside the profile.
// The standard's folder names never contain a separator, so a rejected value
// is a malformed one, not a legitimate one.
func (p Profile) ItemDir(itemType, itemTitle string) (string, error) {
	if strings.TrimSpace(itemType) == "" {
		return "", ErrEmptyItemType
	}
	if strings.TrimSpace(itemTitle) == "" {
		return "", ErrEmptyItemTitle
	}
	if !isFolderName(itemType) {
		return "", fmt.Errorf("%w: %q", ErrBadItemType, itemType)
	}
	if !isFolderName(itemTitle) {
		return "", fmt.Errorf("%w: %q", ErrBadItemTitle, itemTitle)
	}
	return filepath.Join(p.Path, ItemsDirName, itemType, itemTitle), nil
}

// isFolderName reports whether s can only ever name one folder directly inside
// another: no separators of either kind, and not the "." or ".." that would
// point elsewhere. Used for slugs, item types and item titles alike, because
// all three are joined onto a path a caller did not choose.
func isFolderName(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	return !strings.ContainsAny(s, `/\`)
}

// Slugify turns a display name into a folder name: lower-cased, letters and
// digits kept, every other run of characters collapsed to one hyphen, and
// hyphens trimmed from the ends. "Sam's Profile" is "sam-s-profile"; "Sam" and
// "SAM" and "  sam  " are all "sam", which is the point - a name typed on two
// devices should land in the same folder.
//
// Letters and digits are Unicode, not ASCII, so "José" is "josé" rather than
// "jos". The result is empty for a name with no letters or digits at all.
//
// The name is normalised to NFC first, and that is not a nicety: an é can be
// one code point or an e followed by a combining accent, and keyboards on
// different systems produce different forms for the same visible name. Without
// this step the two forms slug to "josé" and "jose" and land in two folders -
// on exactly the two devices the slug exists to reconcile.
func Slugify(name string) string {
	var b strings.Builder
	pendingHyphen := false
	for _, r := range strings.ToLower(norm.NFC.String(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if pendingHyphen && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingHyphen = false
			b.WriteRune(r)
			continue
		}
		pendingHyphen = true
	}
	return b.String()
}
