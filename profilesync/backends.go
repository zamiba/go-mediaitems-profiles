package profilesync

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Timeouts per tool. git on a profile is seconds; rclone on a profile full of
// saves over a slow link can legitimately take a long while, and killing it
// half-way is worse than waiting.
const (
	gitTimeout       = 2 * time.Minute
	rcloneTimeout    = 30 * time.Minute
	syncthingTimeout = 30 * time.Second
)

// git stages everything in the profile, commits if anything changed, and
// pushes if a remote is named.
//
// The identity question: git refuses to commit without user.name and
// user.email, and a fresh machine often has neither. Failing every session
// over that would defeat the purpose, so when the repository has no identity
// the commit is made as "<program> <program@localhost>" - the program that
// asked for it. The commit's job is a timeline of somebody's saves, not
// authorship, and the reason line already says what happened.
func (n *Notifier) git(dir, reason, remote string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()

	if !isGitRepository(dir) {
		return "", ErrNotARepository
	}
	if out, err := n.run(ctx, dir, "git", "add", "-A"); err != nil {
		return out, fmt.Errorf("git add: %w", err)
	}

	// Nothing staged means nothing to commit, which is a success rather than
	// git's non-zero exit and a message to parse.
	status, err := n.run(ctx, dir, "git", "status", "--porcelain")
	if err != nil {
		return status, fmt.Errorf("git status: %w", err)
	}
	var outputs []string
	if status == "" {
		outputs = append(outputs, "nothing to commit")
	} else {
		args := []string{}
		if !n.gitHasIdentity(ctx, dir) {
			args = append(args, "-c", "user.name="+n.program, "-c", "user.email="+n.program+"@localhost")
		}
		args = append(args, "commit", "-q", "-m", commitMessage(n.program, reason))
		out, err := n.run(ctx, dir, "git", args...)
		if err != nil {
			return out, fmt.Errorf("git commit: %w", err)
		}
		outputs = append(outputs, "committed")
	}

	if remote != "" {
		out, err := n.run(ctx, dir, "git", "push", "-q", remote, "HEAD")
		if err != nil {
			return out, fmt.Errorf("git push %s: %w", remote, err)
		}
		outputs = append(outputs, "pushed to "+remote)
	}
	return strings.Join(outputs, "; "), nil
}

// gitHasIdentity reports whether the repository (or the user's global config)
// has a user.name. git exits non-zero when the key is unset, so an error here
// means "no", not "broken".
func (n *Notifier) gitHasIdentity(ctx context.Context, dir string) bool {
	out, err := n.run(ctx, dir, "git", "config", "user.name")
	return err == nil && strings.TrimSpace(out) != ""
}

// commitMessage is what git log shows: the program, then the reason. Kept on
// one line so a log of a profile reads as a timeline.
func commitMessage(program, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "profile changed"
	}
	return program + ": " + reason
}

// rclone copies or mirrors the profile to a remote. verb is "copy" (never
// deletes on the remote) or "sync" (deletes remote files absent locally); the
// two are separate backend kinds so the choice is visible where it is made.
func (n *Notifier) rclone(verb, dir, remote string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), rcloneTimeout)
	defer cancel()
	out, err := n.run(ctx, dir, "rclone", verb, "--quiet", dir, remote)
	if err != nil {
		return out, fmt.Errorf("rclone %s to %s: %w", verb, remote, err)
	}
	if out == "" {
		out = verb + " to " + remote
	}
	return out, nil
}

// syncthingRescan asks a Syncthing instance to rescan the folder holding the
// profile. Syncthing watches the filesystem by default and needs no nudge;
// this is for people who have watching off, and it is the whole of what a
// change means to Syncthing - it does its own transfer.
func (n *Notifier) syncthingRescan(b Backend) (string, error) {
	base := b.URL
	if base == "" {
		base = "http://127.0.0.1:8384"
	}
	endpoint, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("syncthing url %q: %w", base, err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/rest/db/scan"
	endpoint.RawQuery = url.Values{"folder": {b.FolderID}}.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), syncthingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), nil)
	if err != nil {
		return "", err
	}
	if b.APIKey != "" {
		req.Header.Set("X-API-Key", b.APIKey)
	}
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("syncthing rescan: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("syncthing rescan: %s", resp.Status)
	}
	return "rescan requested for folder " + b.FolderID, nil
}
