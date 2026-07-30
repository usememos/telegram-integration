package store

import (
	"fmt"
	"sync"
)

type Store struct {
	Data string

	// saveMu serializes cache-mutation-then-file-save sequences so an older
	// snapshot can't win the rename race and discard a newer entry.
	saveMu sync.Mutex

	userAccessTokenCache sync.Map // map[int64]string
	messageMemoCache     sync.Map // map[messageKey]string
}

func NewStore(data string) *Store {
	return &Store{
		Data: data,

		userAccessTokenCache: sync.Map{},
		messageMemoCache:     sync.Map{},
	}
}

func (s *Store) Init() error {
	if err := s.loadUserAccessTokenMapFromFile(); err != nil {
		return fmt.Errorf("failed to load user access token map from file: %w", err)
	}
	if err := s.loadMessageMemoMapFromFile(); err != nil {
		return fmt.Errorf("failed to load message memo map from file: %w", err)
	}

	return nil
}
