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

// messageKey scopes a Telegram message ID to its chat: message IDs are only
// unique within a chat, not globally.
type messageKey struct {
	chatID    int64
	messageID int64
}

func (s *Store) GetMemoForMessage(chatID, messageID int64) (string, bool) {
	memoName, ok := s.messageMemoCache.Load(messageKey{chatID: chatID, messageID: messageID})
	if !ok {
		return "", false
	}
	return memoName.(string), true
}

func (s *Store) SetMemoForMessage(chatID, messageID int64, memoName string) {
	s.messageMemoCache.Store(messageKey{chatID: chatID, messageID: messageID}, memoName)
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
		if _, err := fmt.Fprintf(writer, "%d:%d:%s\n", entry.key.chatID, entry.key.messageID, entry.memoName); err != nil {
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
		key, memoName := parseMessageMemoLine(line)
		if key.messageID == 0 || memoName == "" {
			continue
		}
		s.messageMemoCache.Store(key, memoName)
	}
	return scanner.Err()
}

func parseMessageMemoLine(line string) (messageKey, string) {
	parts := strings.SplitN(line, ":", 3)
	if len(parts) != 3 {
		return messageKey{}, ""
	}
	chatID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return messageKey{}, ""
	}
	messageID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return messageKey{}, ""
	}
	return messageKey{chatID: chatID, messageID: messageID}, parts[2]
}

type messageMemoEntry struct {
	key      messageKey
	memoName string
}

func (s *Store) snapshotMessageMemoMap() []messageMemoEntry {
	entries := make([]messageMemoEntry, 0)
	s.messageMemoCache.Range(func(key, value interface{}) bool {
		messageKey, ok := key.(messageKey)
		if !ok {
			return true
		}
		memoName, ok := value.(string)
		if !ok {
			return true
		}
		entries = append(entries, messageMemoEntry{key: messageKey, memoName: memoName})
		return true
	})

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].key.chatID != entries[j].key.chatID {
			return entries[i].key.chatID < entries[j].key.chatID
		}
		return entries[i].key.messageID < entries[j].key.messageID
	})

	return entries
}
