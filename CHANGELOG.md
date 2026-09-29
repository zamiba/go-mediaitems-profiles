# Changelog

## v0.4.0 — 2026-09-29

### Changed

- **Requires `go-mediaitems` v0.4.0**, for the `itemtitle` package.
- **Slug resolution uses `itemtitle.PathSafe`**, and this package's own
  `isFolderName` is gone. There is now one implementation of "is this a single
  safe folder name" in the suite rather than two. Slightly stricter, since
  `PathSafe` also rejects control characters; `Slugify` never produced one, so
  nothing that exists can be orphaned by it.

  Slugs deliberately take the **security floor alone** rather than
  `ValidFolderName`: a slug is not an `_itemTitle`. It is reductive by design,
  and it is an identity that already exists in folders on people's machines, so
  holding it to the standard's rule would not prevent a bad profile from being
  made — it would make an existing one unreachable. `Get` and `List` therefore
  still find a profile whose slug is `con`, `.sam` or `sam.`; `Create` is where
  a new slug is judged, and it is stricter.
- **`Profile.ItemDir` now checks both names with `itemtitle.ValidFolderName`**
  — the MediaItem standard's rule — instead of this package's own
  `isFolderName`, which only rejected path separators and `.`/`..`. This
  folder mirrors the item's folder on a storage unit, so a rule that differs
  by one character does not fail: it files one item in two places.
  **This is stricter, and is a behaviour change for callers.** Titles that
  were accepted before and are now refused include a colon or any other
  character Windows forbids, a leading dot, a trailing dot or space, a doubled
  space, a name that is not NFC, and the names Windows reserves for devices.
  PortForge checked its catalogue when the rule was agreed and found nothing
  affected.
- **`ItemDir` normalises both names to NFC** and uses the normalised form in
  the path. It changes no character; it is what stops one item getting two
  folders when one catalogue was authored on a Mac and another on Linux.
  Sanitizing is deliberately not done here — it would strip the separators out
  of a finished `_itemTitle`.
- `ErrBadItemType` and `ErrBadItemTitle` keep their meaning and widen their
  message.

### Added

- **`ErrReservedName`** — `Create` refuses a name whose slug is one Windows
  keeps for a device (`Con`, `Aux`, `Nul`, `Prn`, `Com1`…). The folder cannot
  exist there, so creating one would make a profile that breaks the moment it
  is copied to Windows, which is the one thing profiles are meant to survive.
  `Get` and `List` do **not** apply the rule: a profile that already exists
  stays reachable, because losing somebody's data is worse than the
  portability problem.

## v0.3.0 — 2026-09-28

### Changed

- **`_schemaVersion` in `profile.json` is now the string `"1.0"`**, not the
  number `1`, matching the MediaItem standard's format for the key of the same
  name: `MAJOR.MINOR`, ordered by comparing each part as an integer so `"1.10"`
  is newer than `"1.9"`. A profile is not a MediaItem, but one key name with
  two value types across one suite is a trap for anything generic enough to
  read both.
- **`SchemaVersion` is therefore a string constant**, where it was an untyped
  integer. This is the only breaking change; nothing in the suite referenced
  it.

### Added

- **Profiles written before 2026-09-28 keep working.** A numeric
  `_schemaVersion` reads as that major version with minor `0`, and the file is
  rewritten in the new form the next time anything changes it. There is no
  migration step and nothing to run.
- **A file declaring a newer version keeps it.** This package rewrites whole
  files, so without the guard an older build would relabel a newer profile as
  older than it is while faithfully preserving the fields that made it newer —
  leaving a file with a new shape and no marker saying so.
- A `_schemaVersion` that cannot be read is replaced rather than preserved,
  unlike `_createdAt`: it is this package's own marker, not somebody's data.

## v0.2.0 — 2026-09-21

### Added

- **`Manager.Delete(slug)`** — removes this device's copy of a profile, folder
  and contents, `.git` included; retries once after making the tree writable,
  since git marks its objects read-only and Windows refuses to remove those.
  `ErrNotFound` for a slug with no profile; a stray folder without a
  `profile.json` cannot be deleted through it. Requested by PortForge for a
  delete in its profile modal, on the user's ruling that deletion belongs in
  the module so every program deletes the same way.
- **Profile pictures** — `SetPicture(slug, io.Reader)` accepts PNG, JPEG or
  GIF, centre-crops to square, scales to `PictureSize` (256) and writes
  `picture.png` beside `profile.json`; `RemovePicture(slug)`; `Profile.Picture`
  carries the path or is empty. Adds `golang.org/x/image` for the scaler. The
  file travels with the profile.

### Changed

- Module moved to `MediaItemDocs/go-mediaitems-profiles` beside `go-mediaitems`
  now that nothing builds against the old path.

## v0.1.0 — 2026-09-20

First release. The `profile` package: the profiles folder beside
`storage-units.json`, `profile.json`, slugs as identity, `List` / `Get` /
`Create` / `Ensure` / `Rename`, and `Profile.ItemDir` mirroring a storage
unit's `MediaItems/<ItemType>/<item title>/` layout. No git, no deleting, no
merging.

### Added 2026-09-19

- **`profilesync`** — `Notifier` with `Changed(slug, reason)`, `OnResult`,
  `Close`; backends `git` (implicit when the profile has a `.git`, with a
  `<program> <program@localhost>` fallback identity), `rclone-copy`,
  `rclone-sync` and `syncthing`; `profile-sync.json` beside the profiles
  folder; one run per profile in flight with coalescing. Push-only by design.
  Fourteen tests against a fake command runner.

### Changed 2026-09-20

- `profile.json` is now read and written through `go-mediaitems/jsonfile`
  rather than a private copy of the same code. Behaviour is identical — every
  test passes unchanged — and `meta.go` shrank from 231 lines to 129. Requires
  `go-mediaitems v0.2.0`.

### Changed before release 2026-09-19, on transfer to the module owner

- **`profile.json` meta fields carry the standard's `_` prefix**: `_createdAt`
  and `_createdBy`, beside the existing `_schemaVersion`. `_createdAt` is the
  standard's own field name. The Go field names are unchanged; only the file
  changed, and it was unreleased.
- **`Slugify` normalises to NFC first.** Without it the two encodings of an
  accented letter slugged to different folders — the same visible name on two
  devices would not have met. Adds `golang.org/x/text`.
- **`ItemDir` refuses a type or title that is not a single folder name.** A
  title containing `..` or a separator would have resolved outside the profile.
  New sentinels `ErrBadItemType` and `ErrBadItemTitle`.
- **Names are written without HTML escaping**, so `Rock & Roll` is not stored
  as `Rock \u0026 Roll`. Same rule and helper as `go-mediaitems/storageunit`.
- **A `_createdAt` this package cannot parse is preserved** on rename rather
  than silently dropped.
