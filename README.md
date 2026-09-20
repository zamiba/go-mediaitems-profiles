# `go-mediaitems-profiles`

Profiles for the MediaItem suite: the shared notion of a *person*, as a folder.
Everything the suite records *about* someone — game saves, achievements, what
they have watched — lives in their profile, as opposed to the media itself,
which lives on storage units.

```
import "github.com/zamiba/go-mediaitems-profiles/profile"
```

Profiles are an addition to the MediaItem standard rather than a part of it,
which is why this is its own module. It depends on
[`go-mediaitems`](https://github.com/zamiba/go-mediaitems) for the two things
the suite must agree on: where the configuration folder is, and how a shared
JSON file is read and written (`jsonfile`, so `profile.json` behaves exactly as
`storage-units.json` does).

---

## The layout

```
<config>/MediaItem/profiles/            ← beside storage-units.json
  sam/                                  ← the slug is the identity
    profile.json
    MediaItems/
      VideoGameFanPort/
        Ship of Harkinian · 2022/       ← saves, config; whatever the port keeps
      N64GameRom/
        Super Mario 64 (USA)/
          achievements/…
  portforge/                            ← a program's default profile
    profile.json
```

`profile.json`:

```json
{
  "_schemaVersion": 1,
  "_createdAt": "2026-09-19T10:12:00Z",
  "_createdBy": "portforge",
  "name": "Sam"
}
```

The three `_`-prefixed keys are **meta fields** — they describe the file, not
the person — and carry the prefix for the reason the MediaItem standard gives
it: so they are distinguishable at a glance from data fields, of which there is
one. `_createdAt` is the standard's own field name.

Keys this module does not know are preserved on every write, in the order they
were in, so a program can add its own and a rename from another will not lose
them. So is a `_createdAt` this module cannot parse: unreadable is not the same
as deletable.

## The rules, and why

**A profile is a plain directory.** No database, no lock file, nothing that is
only true on the device that wrote it. A profile can be copied, zipped, put in
git, synced with Syncthing or opened in a file manager, and every program in
the suite will read it.

**Per device, and never synced by this module.** Each device has its own
profiles folder and creates profiles locally. Keeping two devices' copies in
step is whatever the user chooses — git with the other devices as remotes, a
Lexi instance as the meeting point, a sync tool, a copy — and no suite program
needs to know which. **This module never runs git.**

**Same layout as a storage unit.** What a program records about an item goes
under `MediaItems/<ItemType>/<item title>/` inside the profile, mirroring the
item's own folder on a storage unit; `Profile.ItemDir` builds that path. No
program gets a namespace of its own: two programs recording the same kind of
fact write the same files, and agree on their shape.

**Mergeable by design.** A profile from another device is reconciled file by
file. Programs should therefore record facts as small files rather than one
big one — one file per unlocked achievement merges as a union with no
conflicts; one `achievements.json` per item is a conflict every time two
devices unlock something. Binary saves can only be pick-one-side, which is
fine as long as the tool doing the merge says so.

**The slug is the identity, and it never changes.** The folder name is
`Slugify(name)`: Unicode-normalised, lower-cased, letters and digits (Unicode)
kept, every other run collapsed to one hyphen. The same name typed on two
devices gives the same folder, which is how the two are recognised as one
profile when they meet — and the normalisation step is what makes that true
across devices, since an accented letter can be one code point or two
depending on the keyboard that typed it. `Rename` changes the display name
only; a folder rename would break every link and path into it, from every
program on the device.

**Default profiles are ordinary profiles.** A program that wants profiles to
be optional creates one for people who did not ask — PortForge's is named
`portforge` — through `Ensure`, records itself in `createdBy`, and uses it like
any other. It is not hidden: another program on the same device will list it,
which is correct, since it holds real data.

**Nothing here deletes.** A profile is someone's saves. Deleting one is a
deliberate act for a person with a file manager, not a call a program makes.

## Public API

```go
const (
    DirName       = "profiles"
    FileName      = "profile.json"
    ItemsDirName  = "MediaItems"
    SchemaVersion = 1
)

type Profile struct {
    Slug      string    `json:"slug"`      // folder name; identity; never persisted
    Name      string    `json:"name"`      // display label, user-editable
    CreatedAt time.Time `json:"createdAt"` // written to the file as _createdAt
    CreatedBy string    `json:"createdBy,omitempty"` // written as _createdBy
    Path      string    `json:"path"`      // absolute, on this device; never persisted
}

func (p Profile) ItemDir(itemType, itemTitle string) (string, error) // both must be single folder names
func Slugify(name string) string

type Options struct { Dir string } // empty means DefaultDir()

func DefaultDir() (string, error)             // storageunit.DefaultDir()/profiles
func New(opts Options) (*Manager, error)
func Open() (*Manager, error)                 // New(Options{})

func (m *Manager) Dir() string
func (m *Manager) List() ([]Profile, error)   // by slug; folders without profile.json skipped
func (m *Manager) Get(slug string) (Profile, error)
func (m *Manager) Create(name, createdBy string) (Profile, error)
func (m *Manager) Ensure(name, createdBy string) (Profile, error) // get-or-create
func (m *Manager) Rename(slug, name string) error

var (
    ErrEmptyName      // blank, or slugifies to nothing
    ErrExists         // Create: same slug already there
    ErrNotFound
    ErrEmptyItemType
    ErrEmptyItemTitle
    ErrBadItemType    // ItemDir: contains a separator, or is "." or ".."
    ErrBadItemTitle   // ItemDir: same — would resolve outside the profile
)
```

`Create` writes only `profile.json`, and creates it exclusively, so two
programs creating the same profile at once cannot both think they did.
`ItemDir` does not create the folder: the caller does when it has something to
write, so listing a profile shows only the items it holds data for. It refuses
a type or title that is not a single folder name, because a title comes from
catalogue data somebody else wrote and one containing `..` or a separator would
otherwise point outside the profile.

## `profilesync` — sending a changed profile where the user wants it

The `profile` package is a plain directory with no processes behind it. The
processes live here, in a separate package a program imports only if it wants
them.

```go
import "github.com/zamiba/go-mediaitems-profiles/profilesync"

n, err := profilesync.Open(profiles, "portforge", profilesync.Options{})
n.OnResult(func(r profilesync.Result) { /* notice bar */ })
...
n.Changed("sam", "game ended: Ship of Harkinian")   // returns at once
...
n.Close()                                            // waits for in-flight runs
```

**One call, never blocking.** `Changed(slug, reason)` returns immediately and
never panics; every backend's outcome arrives on the callback as a `Result`.
`reason` is free text that becomes the git commit message, so it should say
what happened rather than which program noticed.

**Push-only, always.** `Changed` never pulls, fetches or merges. Reconciling
two devices is a deliberate act with its own tool; a background hook that
merged somebody's saves is the one thing this package must never become.

**Coalesced.** One run per profile at a time. A `Changed` during a run
schedules exactly one more, carrying the latest reason, so a burst of five
events is two runs.

### Backends

Configured in `<config>/MediaItem/profile-sync.json` — beside the profiles
folder, **never inside a profile**, because a remote or an API key is only true
on the device it was set up on:

```json
{
  "version": 1,
  "profiles": {
    "*":   [{ "kind": "syncthing", "apiKey": "…", "folderId": "abcd-1234" }],
    "sam": [
      { "kind": "git", "remote": "lexi" },
      { "kind": "rclone-copy", "remote": "backup:profiles/sam" }
    ]
  }
}
```

`*` entries run for every profile, before the slug's own. The file is read,
never written, by this package.

| kind | what runs | notes |
|---|---|---|
| `git` | `git add -A`, commit if anything changed, `push <remote> HEAD` if `remote` is set | **Implicit with no configuration at all** when the profile folder has a `.git` — so the zero-effort case is `git init` in the profile. Never runs `git init` itself. |
| `rclone-copy` | `rclone copy <profile> <remote>` | Never deletes on the remote. |
| `rclone-sync` | `rclone sync <profile> <remote>` | A mirror: deletes remote files absent locally. A separate kind, not a switch, so the choice is visible where it is made. |
| `syncthing` | `POST /rest/db/scan?folder=<folderId>` | Syncthing watches by default and needs nothing; this is for people with watching off. |

**The git identity.** git refuses to commit without a `user.name` and
`user.email`, and a fresh machine often has neither. Rather than fail every
session, a repository with no identity is committed to as
**`<program> <program@localhost>`** — the program that asked, so PortForge's
commits are `portforge <portforge@localhost>` and Digitalizer's are
`digitalizer <digitalizer@localhost>`. A repository that has an identity keeps
it. A clean tree is "nothing to commit" and a success, not an error.

Tools are shelled out to rather than linked: they already exist on the
machine, their configuration is the user's own, and reconciliation needs a
real three-way merge no Go library provides. A tool that is not installed is a
`Result` whose error satisfies `errors.Is(err, exec.ErrNotFound)`.

## Not here, on purpose

- **Which profile is active.** That is per program — PortForge's active profile
  is in PortForge's own preferences — because each program has its own default.
- **Merging, export and import.** Reconciling two copies of a profile, or
  moving a handful of saves from one profile to another, are later features
  with their own tools. `profilesync` does not do them either; it only pushes.
- **Snapshots.** Left to git, or whatever the user runs.
