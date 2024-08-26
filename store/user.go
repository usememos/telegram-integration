package store

import (
	"bufio"
	"log/slog"
	"os"
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
	// Open the file for writing
	file, err := os.OpenFile(s.Data, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	// Iterate over the user access token map and write each entry to the file
	s.userAccessTokenCache.Range(func(key, value interface{}) bool {
		userID := key.(int64)
		accessToken := value.(string)
		line := strconv.FormatInt(userID, 10) + ":" + accessToken + "\n"
		_, err := file.WriteString(line)
		if err != nil {
			return false
		}
		return true
	})

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
		line := scanner.Text()
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
	parts := strings.Split(line, ":")
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
