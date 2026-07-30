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

func (s *Store) GetMemoForMessage(messageID int64) (string, bool) {
	memoName, ok := s.messageMemoCache.Load(messageID)
	if !ok {
		return "", false
	}
	return memoName.(string), true
}

func (s *Store) SetMemoForMessage(messageID int64, memoName string) {
	s.messageMemoCache.Store(messageID, memoName)
	if err := s.saveMessageMemoMapToFile(); err != nil {
		slog.Error("failed to save message memo map to file", "error", err)
	}
}

func (s *Store) messageMemoMapDataPath() string {
	return s.Data + ".messages"
}

func (s *Store) saveMessageMemoMapToFile() error {
	entries := s.snapshotMessageMemoMap()
	dataPath := s.messageMemoMapDataPath()
	dataDir := filepath.Dir(dataPath)
	tmpFile, err := os.CreateTemp(dataDir, "memogram-messages-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	writer := bufio.NewWriter(tmpFile)
	for _, entry := range entries {
		if _, err := fmt.Fprintf(writer, "%d:%s\n", entry.messageID, entry.memoName); err != nil {
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

	if err := os.Rename(tmpFile.Name(), dataPath); err != nil {
		return fmt.Errorf("replace data file: %w", err)
	}
	return nil
}

func (s *Store) loadMessageMemoMapFromFile() error {
	dataPath := s.messageMemoMapDataPath()
	if _, err := os.Stat(dataPath); os.IsNotExist(err) {
		file, err := os.Create(dataPath)
		if err != nil {
			return err
		}
		defer file.Close()
	}

	file, err := os.Open(dataPath)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		messageID, memoName := parseMessageMemoLine(line)
		if messageID == 0 || memoName == "" {
			continue
		}
		s.messageMemoCache.Store(messageID, memoName)
	}
	return scanner.Err()
}

func parseMessageMemoLine(line string) (int64, string) {
	parts := strings.SplitN(line, ":", 2)
	if len(parts) != 2 {
		return 0, ""
	}
	messageID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, ""
	}
	return messageID, parts[1]
}

type messageMemoEntry struct {
	messageID int64
	memoName  string
}

func (s *Store) snapshotMessageMemoMap() []messageMemoEntry {
	entries := make([]messageMemoEntry, 0)
	s.messageMemoCache.Range(func(key, value interface{}) bool {
		messageID, ok := key.(int64)
		if !ok {
			return true
		}
		memoName, ok := value.(string)
		if !ok {
			return true
		}
		entries = append(entries, messageMemoEntry{messageID: messageID, memoName: memoName})
		return true
	})

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].messageID < entries[j].messageID
	})

	return entries
}
