package profile

import "errors"

// Sentinel errors, so a caller can branch on the case rather than match on
// message text.
var (
	// ErrEmptyName is returned by Create and Rename for a name that is blank,
	// or that slugifies to nothing - a name made only of punctuation has no
	// folder name to live under.
	ErrEmptyName = errors.New("profile: name cannot be empty")

	// ErrExists is returned by Create when a profile with the same slug is
	// already there. Two names that differ only in case or punctuation are
	// the same profile; Ensure is the call for "this one, whether or not it
	// exists yet".
	ErrExists = errors.New("profile: a profile with that name already exists")

	// ErrNotFound is returned by Get and Rename for an unknown slug.
	ErrNotFound = errors.New("profile: no such profile")

	// ErrEmptyItemType and ErrEmptyItemTitle are returned by ItemDir, which
	// would otherwise hand back the profile's MediaItems folder itself - a
	// place a program must never write a single item's data to.
	ErrEmptyItemType  = errors.New("profile: item type cannot be empty")
	ErrEmptyItemTitle = errors.New("profile: item title cannot be empty")

	// ErrBadItemType and ErrBadItemTitle are returned by ItemDir for a value
	// that itemtitle.ValidFolderName rejects: one that would resolve to a path
	// outside the profile, or that is not a name the MediaItem standard could
	// have produced and every filesystem in the suite can hold.
	ErrBadItemType  = errors.New("profile: item type is not a valid folder name")
	ErrBadItemTitle = errors.New("profile: item title is not a valid folder name")

	// ErrReservedName is returned by Create for a name whose slug is one
	// Windows keeps for a device - "Con", "Aux", "Nul", "Prn", "Com1" and the
	// rest. The folder simply cannot exist there, so creating one here would
	// make a profile that stops working the moment it is copied to a Windows
	// machine, which is the one thing profiles are meant to survive. Existing
	// profiles with such a slug are still found and read; only making a new
	// one is refused.
	ErrReservedName = errors.New("profile: that name cannot be used as a folder on Windows")
)
