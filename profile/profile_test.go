package profile

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func openTemp(t *testing.T) *Manager {
	t.Helper()
	m, err := New(Options{Dir: filepath.Join(t.TempDir(), "profiles")})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Sam":             "sam",
		"  sam  ":         "sam",
		"Sam's Profile":   "sam-s-profile",
		"José":            "josé",
		"--- !!! ---":     "",
		"Kid 2 (couch)":   "kid-2-couch",
		"portforge":       "portforge",
		"Ünïcödé Name":    "ünïcödé-name",
		"trailing-":       "trailing",
		"a\tb\nc":         "a-b-c",
		"UPPER and lower": "upper-and-lower",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCreateMakesAFolderNamedByTheSlug(t *testing.T) {
	m := openTemp(t)
	p, err := m.Create("Sam's Profile", "portforge")
	if err != nil {
		t.Fatal(err)
	}
	if p.Slug != "sam-s-profile" || p.Name != "Sam's Profile" || p.CreatedBy != "portforge" {
		t.Errorf("profile = %+v", p)
	}
	if p.Path != filepath.Join(m.Dir(), "sam-s-profile") {
		t.Errorf("path = %s", p.Path)
	}
	if p.CreatedAt.IsZero() {
		t.Error("createdAt not set")
	}
	body, err := os.ReadFile(filepath.Join(p.Path, FileName))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatal(err)
	}
	if got["_schemaVersion"] != float64(SchemaVersion) || got["name"] != "Sam's Profile" || got["_createdBy"] != "portforge" {
		t.Errorf("profile.json = %s", body)
	}
	// Meta fields carry the standard's "_" prefix; the one data field does not.
	for _, meta := range []string{"_schemaVersion", "_createdAt", "_createdBy"} {
		if _, ok := got[meta]; !ok {
			t.Errorf("%s missing from profile.json:\n%s", meta, body)
		}
	}
	for _, old := range []string{"createdAt", "createdBy"} {
		if _, ok := got[old]; ok {
			t.Errorf("%s should be written with the meta-field prefix:\n%s", old, body)
		}
	}
	if _, ok := got["slug"]; ok {
		t.Error("the slug is the folder name and must not be written to the file")
	}
	if _, ok := got["path"]; ok {
		t.Error("the path is only true on this device and must not be written to the file")
	}
	// Nothing else is created: an item folder appears when a program has
	// something to put in it.
	entries, _ := os.ReadDir(p.Path)
	if len(entries) != 1 {
		t.Errorf("a new profile should hold only %s, got %d entries", FileName, len(entries))
	}
}

func TestCreateRefusesADuplicateAndEmptyNames(t *testing.T) {
	m := openTemp(t)
	if _, err := m.Create("Sam", "x"); err != nil {
		t.Fatal(err)
	}
	// Same slug, different spelling: the same profile.
	if _, err := m.Create("  SAM ", "x"); !errors.Is(err, ErrExists) {
		t.Errorf("err = %v, want ErrExists", err)
	}
	for _, name := range []string{"", "   ", "!!!"} {
		if _, err := m.Create(name, "x"); !errors.Is(err, ErrEmptyName) {
			t.Errorf("Create(%q): err = %v, want ErrEmptyName", name, err)
		}
	}
}

func TestEnsureReturnsTheExistingProfile(t *testing.T) {
	m := openTemp(t)
	first, err := m.Ensure("portforge", "portforge")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Rename(first.Slug, "Living room"); err != nil {
		t.Fatal(err)
	}
	again, err := m.Ensure("portforge", "portforge")
	if err != nil {
		t.Fatal(err)
	}
	if again.Slug != first.Slug || again.Name != "Living room" || !again.CreatedAt.Equal(first.CreatedAt) {
		t.Errorf("Ensure should return the profile as it is now, got %+v (first %+v)", again, first)
	}
}

func TestListSkipsFoldersThatAreNotProfiles(t *testing.T) {
	m := openTemp(t)
	for _, n := range []string{"Zed", "alpha"} {
		if _, err := m.Create(n, "t"); err != nil {
			t.Fatal(err)
		}
	}
	os.Mkdir(filepath.Join(m.Dir(), "stray"), 0o755)
	os.Mkdir(filepath.Join(m.Dir(), "broken"), 0o755)
	os.WriteFile(filepath.Join(m.Dir(), "broken", FileName), []byte("{nope"), 0o644)
	os.WriteFile(filepath.Join(m.Dir(), "afile"), []byte(""), 0o644)

	list, err := m.List()
	if err != nil {
		t.Fatal(err)
	}
	var slugs []string
	for _, p := range list {
		slugs = append(slugs, p.Slug)
	}
	if got := strings.Join(slugs, ","); got != "alpha,zed" {
		t.Errorf("List = %s, want alpha,zed", got)
	}
}

func TestRenameChangesTheNameNotTheFolder(t *testing.T) {
	m := openTemp(t)
	p, err := m.Create("Sam", "t")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Rename("sam", "Samantha"); err != nil {
		t.Fatal(err)
	}
	got, err := m.Get("sam")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Samantha" || got.Path != p.Path || !got.CreatedAt.Equal(p.CreatedAt) || got.CreatedBy != "t" {
		t.Errorf("after rename: %+v", got)
	}
	if _, err := m.Get("samantha"); !errors.Is(err, ErrNotFound) {
		t.Error("renaming must not create a folder for the new name")
	}
	if err := m.Rename("sam", " "); !errors.Is(err, ErrEmptyName) {
		t.Errorf("blank name: err = %v", err)
	}
	if err := m.Rename("nobody", "x"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown slug: err = %v", err)
	}
}

// Another program may put keys of its own in profile.json. A rename keeps
// them, in the order they were written, and a rename to the same name leaves
// the file byte-identical.
func TestRenamePreservesWhatOtherProgramsWrote(t *testing.T) {
	m := openTemp(t)
	dir := filepath.Join(m.Dir(), "sam")
	os.MkdirAll(dir, 0o755)
	original := "{\n  \"avatar\": \"sam.png\",\n  \"name\": \"Sam\",\n  \"htpc\": {\n    \"pin\": \"1234\"\n  },\n  \"_schemaVersion\": 1\n}\n"
	file := filepath.Join(dir, FileName)
	os.WriteFile(file, []byte(original), 0o644)

	if err := m.Rename("sam", "Sam"); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(file); string(body) != original {
		t.Errorf("a no-op rename rewrote the file:\n%s", body)
	}

	if err := m.Rename("sam", "Samantha"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(file)
	want := "{\n  \"avatar\": \"sam.png\",\n  \"name\": \"Samantha\",\n  \"htpc\": {\n    \"pin\": \"1234\"\n  },\n  \"_schemaVersion\": 1\n}\n"
	if string(body) != want {
		t.Errorf("after rename:\n%s\nwant:\n%s", body, want)
	}
}

func TestGetRefusesSlugsThatLeaveTheFolder(t *testing.T) {
	m := openTemp(t)
	for _, slug := range []string{"", ".", "..", "../x", "a/b", "sam/"} {
		if _, err := m.Get(slug); !errors.Is(err, ErrNotFound) {
			t.Errorf("Get(%q): err = %v, want ErrNotFound", slug, err)
		}
	}
}

func TestItemDirMirrorsAStorageUnit(t *testing.T) {
	p := Profile{Path: "/p/sam"}
	dir, err := p.ItemDir("VideoGameFanPort", "Ship of Harkinian · 2022")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/p/sam", ItemsDirName, "VideoGameFanPort", "Ship of Harkinian · 2022"); dir != want {
		t.Errorf("ItemDir = %s, want %s", dir, want)
	}
	if _, err := p.ItemDir("", "x"); !errors.Is(err, ErrEmptyItemType) {
		t.Errorf("empty type: %v", err)
	}
	if _, err := p.ItemDir("VideoGameFanPort", " "); !errors.Is(err, ErrEmptyItemTitle) {
		t.Errorf("empty title: %v", err)
	}
}

func TestDefaultDirIsBesideTheStorageUnitList(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	t.Setenv("HOME", "/home/x")
	dir, err := DefaultDir()
	if err != nil {
		t.Skip(err) // not a Linux-shaped config dir here
	}
	if !strings.HasSuffix(dir, filepath.Join("MediaItem", "profiles")) {
		t.Errorf("DefaultDir = %s", dir)
	}
}

// The same visible name must land in the same folder whichever way a keyboard
// encoded it - that is the whole point of the slug being the identity.
func TestSlugifyNormalisesUnicode(t *testing.T) {
	precomposed := "Jos\u00e9" // é as one code point
	decomposed := "Jose\u0301" // e followed by a combining acute accent
	if a, b := Slugify(precomposed), Slugify(decomposed); a != b {
		t.Errorf("the two encodings of José slug differently: %q vs %q", a, b)
	}
	if got := Slugify(decomposed); got != "josé" {
		t.Errorf("Slugify(decomposed José) = %q, want %q", got, "josé")
	}
}

// An item title comes from catalogue data somebody else wrote. One containing
// a separator must not resolve to a path outside the profile.
func TestItemDirRefusesPathsThatLeaveTheProfile(t *testing.T) {
	p := Profile{Path: "/p/sam"}
	for _, title := range []string{"../../etc", "a/b", `a\b`, ".", ".."} {
		if _, err := p.ItemDir("VideoGame", title); !errors.Is(err, ErrBadItemTitle) {
			t.Errorf("ItemDir(title=%q): err = %v, want ErrBadItemTitle", title, err)
		}
	}
	for _, typ := range []string{"../x", "Video/Game", ".."} {
		if _, err := p.ItemDir(typ, "Title · 2001"); !errors.Is(err, ErrBadItemType) {
			t.Errorf("ItemDir(type=%q): err = %v, want ErrBadItemType", typ, err)
		}
	}
	// The standard's own folder names - separator, spaces, parentheses,
	// underscore disambiguator - are all single folder names and must pass.
	for _, title := range []string{"Super Mario 64 · 1996", "Blue Dragon (USA) (Disc 2)", "The Fellowship · 2001_tt0120737"} {
		if _, err := p.ItemDir("VideoGame", title); err != nil {
			t.Errorf("ItemDir(title=%q) rejected a legitimate title: %v", title, err)
		}
	}
}

// profile.json is read by people, so a name with an ampersand is written as
// itself rather than as the escape sequence Go's encoder emits by default.
func TestNamesAreNotHTMLEscaped(t *testing.T) {
	m := openTemp(t)
	p, err := m.Create("Rock & Roll <Sam>", "t")
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(p.Path, FileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `"Rock & Roll <Sam>"`) {
		t.Errorf("the name was escaped rather than written literally:\n%s", body)
	}
	for _, escape := range []string{`\u0026`, `\u003c`, `\u003e`} {
		if strings.Contains(string(body), escape) {
			t.Errorf("found the HTML escape %s in profile.json:\n%s", escape, body)
		}
	}
}

// A _createdAt this package cannot parse is still somebody's data. A rename
// writes it back untouched rather than dropping it.
func TestUnparseableCreatedAtSurvivesARename(t *testing.T) {
	m := openTemp(t)
	dir := filepath.Join(m.Dir(), "sam")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, FileName)
	os.WriteFile(file, []byte("{\n  \"name\": \"Sam\",\n  \"_createdAt\": \"last tuesday\"\n}\n"), 0o644)

	if err := m.Rename("sam", "Samantha"); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(file)
	if !strings.Contains(string(body), `"_createdAt": "last tuesday"`) {
		t.Errorf("an unparseable _createdAt was dropped or rewritten:\n%s", body)
	}
	got, err := m.Get("sam")
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.IsZero() {
		t.Errorf("an unparseable _createdAt should read as zero, got %v", got.CreatedAt)
	}
}
