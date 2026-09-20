# Changelog

## v0.1.0 — Unreleased

First release. The `profile` package: the profiles folder beside
`storage-units.json`, `profile.json`, slugs as identity, `List` / `Get` /
`Create` / `Ensure` / `Rename`, and `Profile.ItemDir` mirroring a storage
unit's `MediaItems/<ItemType>/<item title>/` layout. No git, no deleting, no
merging.

### Added (2026-09-19)

- **`profilesync`** — `Notifier` with `Changed(slug, reason)`, `OnResult`,
  `Close`; backends `git` (implicit when the profile has a `.git`, with a
  `<program> <program@localhost>` fallback identity), `rclone-copy`,
  `rclone-sync` and `syncthing`; `profile-sync.json` beside the profiles
  folder; one run per profile in flight with coalescing. Push-only by design.
  Fourteen tests against a fake command runner.

### Changed (2026-09-20)

- `profile.json` is now read and written through `go-mediaitems/jsonfile`
  rather than a private copy of the same code. Behaviour is identical — every
  test passes unchanged — and `meta.go` shrank from 231 lines to 129. Requires
  `go-mediaitems v0.2.0`.

### Changed before release (2026-09-19, on transfer to the module owner)

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
