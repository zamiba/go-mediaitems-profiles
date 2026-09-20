package profilesync

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zamiba/go-mediaitems-profiles/profile"
)

// fakeRunner records every command and answers from a script keyed on the
// command's first two words ("git status"), so a test can say what git would
// have said without git being installed or a repository existing.
type fakeRunner struct {
	mu    sync.Mutex
	calls []string
	reply map[string]struct {
		out string
		err error
	}
	block chan struct{} // if set, every call waits on it (for coalescing tests)
}

func (f *fakeRunner) run(_ context.Context, _ string, name string, args ...string) (string, error) {
	if f.block != nil {
		<-f.block
	}
	line := name + " " + strings.Join(args, " ")
	f.mu.Lock()
	f.calls = append(f.calls, line)
	f.mu.Unlock()
	key := name
	if len(args) > 0 {
		// skip -c pairs to find the verb
		i := 0
		for i+1 < len(args) && args[i] == "-c" {
			i += 2
		}
		if i < len(args) {
			key += " " + args[i]
		}
	}
	if r, ok := f.reply[key]; ok {
		return r.out, r.err
	}
	return "", nil
}

func (f *fakeRunner) lines() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

type harness struct {
	t       *testing.T
	m       *profile.Manager
	n       *Notifier
	fake    *fakeRunner
	results chan Result
}

func newHarness(t *testing.T, config string) *harness {
	t.Helper()
	root := t.TempDir()
	m, err := profile.New(profile.Options{Dir: filepath.Join(root, "profiles")})
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, FileName)
	if config != "" {
		if err := os.WriteFile(cfgPath, []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	n, err := Open(m, "portforge", Options{ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRunner{reply: map[string]struct {
		out string
		err error
	}{}}
	n.run = fake.run
	h := &harness{t: t, m: m, n: n, fake: fake, results: make(chan Result, 64)}
	n.OnResult(func(r Result) { h.results <- r })
	return h
}

func (h *harness) profileWithGit(name string) profile.Profile {
	h.t.Helper()
	p, err := h.m.Create(name, "portforge")
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(p.Path, ".git"), 0o755); err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) next() Result {
	h.t.Helper()
	select {
	case r := <-h.results:
		return r
	case <-time.After(5 * time.Second):
		h.t.Fatal("no result within 5s")
		return Result{}
	}
}

func (h *harness) expectNoMore() {
	h.t.Helper()
	h.n.Close()
	select {
	case r := <-h.results:
		h.t.Errorf("unexpected extra result: %+v", r)
	default:
	}
}

func TestImplicitGitCommitsWithFallbackIdentity(t *testing.T) {
	h := newHarness(t, "")
	h.profileWithGit("Sam")
	h.fake.reply["git status"] = struct {
		out string
		err error
	}{out: " M saves/slot1.sav"}
	h.fake.reply["git config"] = struct {
		out string
		err error
	}{err: errors.New("exit status 1")} // no identity configured

	h.n.Changed("sam", "game ended: Ship of Harkinian")
	r := h.next()
	if r.Err != nil || r.Kind != KindGit || r.Slug != "sam" {
		t.Fatalf("result = %+v", r)
	}
	if r.Output != "committed" {
		t.Errorf("output = %q", r.Output)
	}
	calls := h.fake.lines()
	want := []string{
		"git add -A",
		"git status --porcelain",
		"git config user.name",
		"git -c user.name=portforge -c user.email=portforge@localhost commit -q -m portforge: game ended: Ship of Harkinian",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("calls:\n%s\nwant:\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
	h.expectNoMore()
}

func TestGitUsesTheRepositorysOwnIdentityWhenItHasOne(t *testing.T) {
	h := newHarness(t, "")
	h.profileWithGit("Sam")
	h.fake.reply["git status"] = struct {
		out string
		err error
	}{out: "?? new.sav"}
	h.fake.reply["git config"] = struct {
		out string
		err error
	}{out: "Sam Person"}

	h.n.Changed("sam", "x")
	h.next()
	for _, c := range h.fake.lines() {
		if strings.Contains(c, "user.email=") {
			t.Errorf("fallback identity used although one is configured: %s", c)
		}
	}
	h.expectNoMore()
}

func TestNothingToCommitIsSuccessNotFailure(t *testing.T) {
	h := newHarness(t, "")
	h.profileWithGit("Sam")
	// git status returns nothing: clean tree.
	h.n.Changed("sam", "x")
	r := h.next()
	if r.Err != nil || r.Output != "nothing to commit" {
		t.Errorf("result = %+v", r)
	}
	for _, c := range h.fake.lines() {
		if strings.Contains(c, "commit") {
			t.Errorf("committed on a clean tree: %s", c)
		}
	}
	h.expectNoMore()
}

func TestExplicitGitBackendPushesAndReplacesImplicit(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{"sam":[{"kind":"git","remote":"lexi"}]}}`)
	h.profileWithGit("Sam")
	h.fake.reply["git status"] = struct {
		out string
		err error
	}{out: " M a"}

	h.n.Changed("sam", "x")
	r := h.next()
	if r.Err != nil || !strings.Contains(r.Output, "pushed to lexi") {
		t.Errorf("result = %+v", r)
	}
	pushes, commits := 0, 0
	for _, c := range h.fake.lines() {
		if strings.HasPrefix(c, "git push -q lexi HEAD") {
			pushes++
		}
		if strings.Contains(c, " commit ") {
			commits++
		}
	}
	if pushes != 1 || commits != 1 {
		t.Errorf("pushes=%d commits=%d, want 1 and 1 (implicit git must not double-commit)", pushes, commits)
	}
	h.expectNoMore()
}

func TestGitBackendOnANonRepositoryIsAnError(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{"*":[{"kind":"git"}]}}`)
	if _, err := h.m.Create("Plain", "t"); err != nil {
		t.Fatal(err)
	}
	h.n.Changed("plain", "x")
	r := h.next()
	if !errors.Is(r.Err, ErrNotARepository) {
		t.Errorf("err = %v, want ErrNotARepository", r.Err)
	}
	if len(h.fake.lines()) != 0 {
		t.Errorf("git was run on a non-repository: %v", h.fake.lines())
	}
	h.expectNoMore()
}

func TestNoGitAndNoConfigMeansNothingHappens(t *testing.T) {
	h := newHarness(t, "")
	if _, err := h.m.Create("Plain", "t"); err != nil {
		t.Fatal(err)
	}
	h.n.Changed("plain", "x")
	h.expectNoMore()
}

func TestRcloneCopyAndSyncAreDistinctVerbs(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{
		"sam":[{"kind":"rclone-copy","remote":"backup:profiles/sam"},{"kind":"rclone-sync","remote":"mirror:sam"}]}}`)
	p, err := h.m.Create("Sam", "t")
	if err != nil {
		t.Fatal(err)
	}
	h.n.Changed("sam", "x")
	first, second := h.next(), h.next()
	if first.Kind != KindRcloneCopy || second.Kind != KindRcloneSync || first.Err != nil || second.Err != nil {
		t.Errorf("results = %+v / %+v", first, second)
	}
	calls := h.fake.lines()
	if len(calls) != 2 || calls[0] != "rclone copy --quiet "+p.Path+" backup:profiles/sam" || calls[1] != "rclone sync --quiet "+p.Path+" mirror:sam" {
		t.Errorf("calls = %v", calls)
	}
	h.expectNoMore()
}

func TestStarEntriesRunBeforeTheSlugsOwn(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{
		"*":[{"kind":"rclone-copy","remote":"all:"}],
		"sam":[{"kind":"rclone-copy","remote":"mine:"}]}}`)
	if _, err := h.m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	h.n.Changed("sam", "x")
	h.next()
	h.next()
	calls := h.fake.lines()
	if len(calls) != 2 || !strings.HasSuffix(calls[0], " all:") || !strings.HasSuffix(calls[1], " mine:") {
		t.Errorf("calls = %v", calls)
	}
	h.expectNoMore()
}

func TestToolFailureIsAResultNotAPanic(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{"sam":[{"kind":"rclone-copy","remote":"x:"}]}}`)
	if _, err := h.m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	h.fake.reply["rclone copy"] = struct {
		out string
		err error
	}{out: "Failed to create file system", err: errors.New("exit status 1")}
	h.n.Changed("sam", "x")
	r := h.next()
	if r.Err == nil || !strings.Contains(r.Err.Error(), "rclone copy to x:") || r.Output != "Failed to create file system" {
		t.Errorf("result = %+v", r)
	}
	h.expectNoMore()
}

func TestUnknownSlugAndClosedNotifier(t *testing.T) {
	h := newHarness(t, "")
	h.n.Changed("nobody", "x")
	if r := h.next(); !errors.Is(r.Err, ErrNotFound) {
		t.Errorf("unknown slug: err = %v", r.Err)
	}
	h.n.Close()
	h.n.Changed("sam", "x")
	if r := h.next(); !errors.Is(r.Err, ErrClosed) {
		t.Errorf("after Close: err = %v", r.Err)
	}
}

// Five changes arriving while a run is in flight are one more run, not five.
func TestChangesDuringARunCoalesceIntoOneMore(t *testing.T) {
	h := newHarness(t, `{"version":1,"profiles":{"sam":[{"kind":"rclone-copy","remote":"x:"}]}}`)
	if _, err := h.m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	h.fake.block = make(chan struct{})

	h.n.Changed("sam", "first")
	// Let the first run start and block inside rclone.
	deadline := time.Now().Add(2 * time.Second)
	for len(h.fake.lines()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	for i := range 5 {
		h.n.Changed("sam", "burst "+string(rune('a'+i)))
	}
	close(h.fake.block) // release everything

	first := h.next()
	second := h.next()
	if first.Reason != "first" || second.Reason != "burst e" {
		t.Errorf("reasons = %q, %q; want first, then the latest of the burst", first.Reason, second.Reason)
	}
	h.expectNoMore()
	if got := len(h.fake.lines()); got != 2 {
		t.Errorf("rclone ran %d times, want 2", got)
	}
}

func TestSyncthingRescanHitsTheEndpoint(t *testing.T) {
	var got struct {
		path, query, key string
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path, got.query, got.key = r.URL.Path, r.URL.RawQuery, r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	h := newHarness(t, `{"version":1,"profiles":{"sam":[{"kind":"syncthing","url":"`+srv.URL+`","apiKey":"k","folderId":"abc-123"}]}}`)
	if _, err := h.m.Create("Sam", "t"); err != nil {
		t.Fatal(err)
	}
	h.n.Changed("sam", "x")
	r := h.next()
	if r.Err != nil || got.path != "/rest/db/scan" || got.query != "folder=abc-123" || got.key != "k" {
		t.Errorf("result = %+v; request = %+v", r, got)
	}
	h.expectNoMore()
}

func TestConfigValidation(t *testing.T) {
	root := t.TempDir()
	m, _ := profile.New(profile.Options{Dir: filepath.Join(root, "profiles")})
	cases := map[string]string{
		"unknown kind":        `{"profiles":{"*":[{"kind":"dropbox"}]}}`,
		"rclone no remote":    `{"profiles":{"*":[{"kind":"rclone-copy"}]}}`,
		"syncthing no folder": `{"profiles":{"*":[{"kind":"syncthing"}]}}`,
		"not json":            `{nope`,
	}
	for name, body := range cases {
		path := filepath.Join(root, name+".json")
		os.WriteFile(path, []byte(body), 0o644)
		if _, err := Open(m, "portforge", Options{ConfigPath: path}); err == nil {
			t.Errorf("%s: Open should fail", name)
		}
	}
	// Missing file and empty program name.
	if _, err := Open(m, "portforge", Options{ConfigPath: filepath.Join(root, "absent.json")}); err != nil {
		t.Errorf("missing config should be fine: %v", err)
	}
	if _, err := Open(m, "  ", Options{}); err == nil {
		t.Error("empty program name should fail")
	}
}

func TestCommitMessage(t *testing.T) {
	if got := commitMessage("digitalizer", "  watched: Pilot · S01E01 "); got != "digitalizer: watched: Pilot · S01E01" {
		t.Errorf("got %q", got)
	}
	if got := commitMessage("portforge", ""); got != "portforge: profile changed" {
		t.Errorf("got %q", got)
	}
}
