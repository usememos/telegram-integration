// Package store persists each Telegram user's Memos access token and
// settings.
//
// The data file holds one JSON object per line:
//
//	{"user":42,"token":"<access token>","space":"spaces/team","spaceTitle":"Team"}
//
// Earlier versions wrote "<telegram user id>:<access token>" lines, which are
// still read and are rewritten as JSON on the next change. Blank lines, "#"
// comments and malformed lines are skipped when reading, and the file is
// rewritten in full, sorted by user ID, on every change.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// ErrNotBound reports a Telegram user without an access token.
var ErrNotBound = errors.New("user has not linked a Memos account")

// Space is a user's default Memos space for new memos.
type Space struct {
	// Name is the resource name, such as "spaces/team".
	Name string
	// Title is the space's title when the user chose it, for display.
	Title string
}

type user struct {
	token string
	space Space
}

// record is one line of the data file.
type record struct {
	User       int64  `json:"user"`
	Token      string `json:"token"`
	Space      string `json:"space,omitempty"`
	SpaceTitle string `json:"spaceTitle,omitempty"`
}

type Store struct {
	path string

	// mu serializes writers so that concurrent changes cannot overwrite each
	// other's saved file.
	mu    sync.RWMutex
	users map[int64]user
}

// Open loads the data file at path, creating an empty one readable only by
// its owner if it does not exist.
func Open(path string) (*Store, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve data file path: %w", err)
	}
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			return nil, fmt.Errorf("create data file: %w", err)
		}
		if err := file.Close(); err != nil {
			return nil, fmt.Errorf("create data file: %w", err)
		}
	case err != nil:
		return nil, fmt.Errorf("access data file: %w", err)
	case info.IsDir():
		return nil, fmt.Errorf("data file %s is a directory", path)
	}

	s := &Store{path: path, users: map[int64]user{}}
	if err := s.load(); err != nil {
		return nil, fmt.Errorf("load data file: %w", err)
	}
	return s, nil
}

// AccessToken returns the access token bound to a Telegram user.
func (s *Store) AccessToken(userID int64) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, ok := s.users[userID]
	return u.token, ok
}

// DefaultSpace returns the user's default space, or the zero Space if there
// is none.
func (s *Store) DefaultSpace(userID int64) Space {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[userID].space
}

// SetAccessToken binds a Telegram user to an access token and saves the data
// file. A rebind keeps the user's settings. If saving fails, the previous
// binding is kept.
func (s *Store) SetAccessToken(userID int64, accessToken string) error {
	return s.update(userID, true, func(u *user) { u.token = accessToken })
}

// SetDefaultSpace sets, or with the zero Space clears, the space for the
// user's new memos. The user must be bound. If saving fails, the previous
// setting is kept.
func (s *Store) SetDefaultSpace(userID int64, space Space) error {
	return s.update(userID, false, func(u *user) { u.space = space })
}

func (s *Store) update(userID int64, create bool, change func(*user)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	previous, existed := s.users[userID]
	if !existed && !create {
		return ErrNotBound
	}
	updated := previous
	change(&updated)
	s.users[userID] = updated
	if err := s.save(); err != nil {
		if existed {
			s.users[userID] = previous
		} else {
			delete(s.users, userID)
		}
		return err
	}
	return nil
}

func (s *Store) load() error {
	file, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if userID, u, ok := parseLine(line); ok {
			s.users[userID] = u
		}
	}
	return scanner.Err()
}

// save atomically replaces the data file. The caller must hold s.mu.
func (s *Store) save() error {
	tmpFile, err := os.CreateTemp(filepath.Dir(s.path), "memogram-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	// Fails harmlessly once the file has been renamed into place.
	defer func() { _ = os.Remove(tmpFile.Name()) }()

	userIDs := slices.Sorted(maps.Keys(s.users))
	writer := bufio.NewWriter(tmpFile)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	for _, userID := range userIDs {
		u := s.users[userID]
		if err := encoder.Encode(record{User: userID, Token: u.token, Space: u.space.Name, SpaceTitle: u.space.Title}); err != nil {
			_ = tmpFile.Close()
			return fmt.Errorf("write data file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("flush data file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("sync data file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close data file: %w", err)
	}
	if err := os.Rename(tmpFile.Name(), s.path); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}
	return nil
}

// parseLine reads a JSON record, or a legacy "<user id>:<token>" line whose
// token may contain colons.
func parseLine(line string) (int64, user, bool) {
	if strings.HasPrefix(line, "{") {
		var r record
		if err := json.Unmarshal([]byte(line), &r); err != nil || r.User == 0 || r.Token == "" {
			return 0, user{}, false
		}
		return r.User, user{token: r.Token, space: Space{Name: r.Space, Title: r.SpaceTitle}}, true
	}
	rawID, token, ok := strings.Cut(line, ":")
	if !ok || token == "" {
		return 0, user{}, false
	}
	userID, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || userID == 0 {
		return 0, user{}, false
	}
	return userID, user{token: token}, true
}
