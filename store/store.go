package store

import (
	"sync"

	"github.com/pkg/errors"
)

type Store struct {
	Data string

	userAccessTokenCache sync.Map // map[int64]string
}

func NewStore(data string) *Store {
	return &Store{
		Data: data,

		userAccessTokenCache: sync.Map{},
	}
}

func (s *Store) Init() error {
	if err := s.loadUserAccessTokenMapFromFile(); err != nil {
		return errors.Wrap(err, "failed to load user access token map from file")
	}

	return nil
}
