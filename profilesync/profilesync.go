// Package profilesync sends a changed profile to wherever the user has asked
// for it to go, without the program that changed it having to wait or care
// how.
//
// The profile package is deliberately a plain directory with no processes
// behind it, and this package is where the processes live. A program calls
// Changed once, when something in a profile has changed - a play session
// ended, an episode was watched - and this package commits it to git, hands it
// to rclone, or nudges Syncthing, in the background, reporting each outcome
// through a callback the program registered for its notice bar.
//
// Three rules shape everything here:
//
//   - **Push-only.** Changed never pulls, fetches or merges. Reconciling two
//     devices' copies of a profile is a deliberate act with its own tool and
//     its own message saying what it did. A background hook that merged
//     somebody's saves would be the one thing this package must never become.
//   - **Never blocks the caller, never panics.** A failing backend is a Result
//     with an error in it, delivered to the callback. The game already ended;
//     nothing about a backup being slow or broken should reach the person
//     until they look.
//   - **Coalesced.** One run per profile at a time. A Changed that arrives
//     while a run is in flight schedules exactly one more, so five events in a
//     burst are two runs, not five.
//
// Backends shell out - git, rclone - rather than link libraries, because the
// tools already exist on the machine, their configuration is the user's own,
// and reconciliation needs a real three-way merge that no Go library provides.
package profilesync

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zamiba/go-mediaitems-profiles/profile"
)

// Result is the outcome of one backend step for one change.
type Result struct {
	Slug   string
	Reason string
	Kind   string // the backend kind, or "git" for an implicit commit

	// Err is nil on success. Callers should test with errors.Is where a
	// sentinel exists: ErrNotFound for an unknown slug, ErrClosed after Close,
	// exec.ErrNotFound when the tool is not installed.
	Err error

	// Output is the tool's trimmed combined output, for a notice bar or a log.
	// Empty on success for tools that say nothing.
	Output string

	Duration time.Duration
}

var (
	// ErrClosed is delivered for a Changed that arrives after Close.
	ErrClosed = errors.New("profilesync: notifier is closed")

	// ErrNotFound is delivered for a slug with no profile.
	ErrNotFound = profile.ErrNotFound

	// ErrNotARepository is delivered for a git backend configured on a
	// profile that has no .git. The package never runs git init: making a
	// folder into a repository is the person's decision.
	ErrNotARepository = errors.New("profilesync: profile is not a git repository")
)

// Options configures a Notifier.
type Options struct {
	// ConfigPath overrides where profile-sync.json is read from. Empty means
	// beside the profiles folder, which is where every suite program on the
	// device must read it from.
	ConfigPath string
}

// Notifier sends changed profiles to their backends. Construct one per
// program with Open and keep it for the life of the process; call Close on
// the way out so in-flight runs finish.
type Notifier struct {
	profiles *profile.Manager
	program  string
	cfg      Config

	mu       sync.Mutex
	onResult func(Result)
	state    map[string]*slugState
	closed   bool
	wg       sync.WaitGroup

	// Seams for tests. run executes a command in dir and returns its combined
	// output; httpClient talks to Syncthing.
	run        runner
	httpClient *http.Client
	now        func() time.Time
}

type slugState struct {
	running bool
	pending *change
}

type change struct {
	slug, reason string
}

type runner func(ctx context.Context, dir, name string, args ...string) (string, error)

// Open reads profile-sync.json and returns a Notifier for program - the name
// the calling program goes by in the suite, "portforge" or "digitalizer" -
// which becomes the git author when the repository has none configured.
//
// A missing configuration file is not an error. Implicit git needs no
// configuration at all.
func Open(m *profile.Manager, program string, opts Options) (*Notifier, error) {
	if m == nil {
		return nil, errors.New("profilesync: nil profile manager")
	}
	program = strings.TrimSpace(program)
	if program == "" {
		return nil, errors.New("profilesync: program name cannot be empty")
	}
	path := opts.ConfigPath
	if path == "" {
		path = filepath.Join(filepath.Dir(m.Dir()), FileName)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		profiles:   m,
		program:    program,
		cfg:        cfg,
		state:      make(map[string]*slugState),
		run:        execRunner,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		now:        time.Now,
	}, nil
}

// OnResult registers the function that receives every Result. It is called
// from a background goroutine, one call per backend step, so it must be safe
// to call concurrently with the program's own work - typically it posts to a
// channel or a UI event bus. Register it once, before the first Changed.
func (n *Notifier) OnResult(fn func(Result)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onResult = fn
}

// Changed records that something in the profile has changed and schedules
// its backends. It returns at once. reason is free text - "game ended: Ship
// of Harkinian" - and becomes the git commit message, so it should say what
// happened rather than which program noticed.
//
// One run per slug is in flight at a time; a Changed during a run schedules
// exactly one more run after it, carrying the latest reason.
func (n *Notifier) Changed(slug, reason string) {
	n.mu.Lock()
	if n.closed {
		n.mu.Unlock()
		n.deliver(Result{Slug: slug, Reason: reason, Err: ErrClosed})
		return
	}
	st := n.state[slug]
	if st == nil {
		st = &slugState{}
		n.state[slug] = st
	}
	c := &change{slug: slug, reason: reason}
	if st.running {
		st.pending = c // the latest reason wins; the run that follows covers both
		n.mu.Unlock()
		return
	}
	st.running = true
	n.wg.Add(1)
	n.mu.Unlock()
	go n.loop(st, c)
}

// Close waits for every in-flight run to finish. Changed calls after Close
// deliver ErrClosed rather than starting anything.
func (n *Notifier) Close() error {
	n.mu.Lock()
	n.closed = true
	n.mu.Unlock()
	n.wg.Wait()
	return nil
}

func (n *Notifier) loop(st *slugState, c *change) {
	defer n.wg.Done()
	for c != nil {
		n.runOnce(*c)
		n.mu.Lock()
		c, st.pending = st.pending, nil
		if c == nil {
			st.running = false
		}
		n.mu.Unlock()
	}
}

func (n *Notifier) runOnce(c change) {
	p, err := n.profiles.Get(c.slug)
	if err != nil {
		n.deliver(Result{Slug: c.slug, Reason: c.reason, Err: err})
		return
	}
	backends := n.cfg.backendsFor(c.slug)

	// Implicit git: a profile that is a repository is committed on every
	// change even with no configuration, so the zero-effort case is "git init
	// in the profile folder". An explicit git backend replaces the implicit
	// one rather than duplicating its commit.
	if !hasGitBackend(backends) && isGitRepository(p.Path) {
		backends = append([]Backend{{Kind: KindGit}}, backends...)
	}

	for _, b := range backends {
		n.deliver(n.runBackend(c, p, b))
	}
}

func (n *Notifier) runBackend(c change, p profile.Profile, b Backend) Result {
	started := n.now()
	var out string
	var err error
	switch b.Kind {
	case KindGit:
		out, err = n.git(p.Path, c.reason, b.Remote)
	case KindRcloneCopy:
		out, err = n.rclone("copy", p.Path, b.Remote)
	case KindRcloneSync:
		out, err = n.rclone("sync", p.Path, b.Remote)
	case KindSyncthing:
		out, err = n.syncthingRescan(b)
	default:
		err = fmt.Errorf("profilesync: unknown backend kind %q", b.Kind)
	}
	return Result{Slug: c.slug, Reason: c.reason, Kind: b.Kind, Err: err, Output: out, Duration: n.now().Sub(started)}
}

func (n *Notifier) deliver(r Result) {
	n.mu.Lock()
	fn := n.onResult
	n.mu.Unlock()
	if fn != nil {
		fn(r)
	}
}

func hasGitBackend(bs []Backend) bool {
	for _, b := range bs {
		if b.Kind == KindGit {
			return true
		}
	}
	return false
}

// isGitRepository reports whether dir has a .git entry. A file rather than a
// directory is a worktree's pointer and counts.
func isGitRepository(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// execRunner runs a command with a timeout and returns its combined output.
// LC_ALL=C keeps tool messages in English so that the few we inspect are
// stable across locales.
func execRunner(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
