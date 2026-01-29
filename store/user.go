package store

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// GetUserAccessToken returns the access token for the user.
func (s *Store) GetUserAccessToken(userID int64) (string, bool) {
	accessToken, ok := s.userAccessTokenCache.Load(userID)
	if !ok {
		return "", false
	}
	return accessToken.(string), true
}

// SetUserAccessToken sets the access token for the user.
func (s *Store) SetUserAccessToken(userID int64, accessToken string) {
	s.userAccessTokenCache.Store(userID, accessToken)
	if err := s.SaveUserAccessTokenMapToFile(); err != nil {
		slog.Error("failed to save user access token map to file", "error", err)
	}
}

// SaveUserAccessTokenMapToFile saves the user access token map to a data file.
func (s *Store) SaveUserAccessTokenMapToFile() error {
	entries := s.snapshotAccessTokens()
	dataDir := filepath.Dir(s.Data)
	tmpFile, err := os.CreateTemp(dataDir, "memogram-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	writer := bufio.NewWriter(tmpFile)
	for _, entry := range entries {
		if _, err := fmt.Fprintf(writer, "%d:%s\n", entry.userID, entry.accessToken); err != nil {
			tmpFile.Close()
			return fmt.Errorf("write data file: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("flush data file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		tmpFile.Close()
		return fmt.Errorf("sync data file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close data file: %w", err)
	}

	if err := os.Rename(tmpFile.Name(), s.Data); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}
	return nil
}

func (s *Store) loadUserAccessTokenMapFromFile() error {
	// Check if the file exists
	if _, err := os.Stat(s.Data); os.IsNotExist(err) {
		// Create the file if it doesn't exist
		file, err := os.Create(s.Data)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	// Open the file
	file, err := os.Open(s.Data)
	if err != nil {
		return err
	}
	defer file.Close()

	// Read the file line by line
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse the line and extract the user ID and access token
		userID, accessToken := parseLine(line)
		if userID == 0 || accessToken == "" {
			continue
		}
		// Store the user ID and access token in the cache
		s.userAccessTokenCache.Store(userID, accessToken)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func parseLine(line string) (int64, string) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return 0, ""
	}
	userIDStr := parts[0]
	accessToken := parts[1]
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		return 0, ""
	}
	return userID, accessToken
}

type userAccessTokenEntry struct {
	userID      int64
	accessToken string
}

func (s *Store) snapshotAccessTokens() []userAccessTokenEntry {
	entries := make([]userAccessTokenEntry, 0)
	s.userAccessTokenCache.Range(func(key, value interface{}) bool {
		userID, ok := key.(int64)
		if !ok {
			return true
		}
		accessToken, ok := value.(string)
		if !ok {
			return true
		}
		entries = append(entries, userAccessTokenEntry{
			userID:      userID,
			accessToken: accessToken,
		})
		return true
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].userID < entries[j].userID
	})

	return entries
}
