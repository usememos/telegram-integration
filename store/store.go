package store

import (
	"fmt"
	"sync"
)

type Store struct {
	Data string

	userAccessTokenCache sync.Map // map[int64]string
	messageMemoCache     sync.Map // map[int64]string, telegram message ID -> memo resource name
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
