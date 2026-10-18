package bot

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/usememos/memogram/internal/memosapi"
)

func TestAlbumPartsShareOneMemo(t *testing.T) {
	groups := newMediaGroups(time.Hour)
	var creations atomic.Int32
	release := make(chan struct{})
	create := func() (*memosapi.Memo, error) {
		creations.Add(1)
		<-release
		return &memosapi.Memo{Name: "memos/album"}, nil
	}
	var wg sync.WaitGroup
	results := make(chan string, 5)
	for range 5 {
		wg.Go(func() {
			memo, err := groups.memo(context.Background(), "1:album", create)
			if err != nil {
				t.Error(err)
				return
			}
			results <- memo.Name
		})
	}
	// Another album must not wait for the first one's creation.
	if memo, err := groups.memo(context.Background(), "1:other", func() (*memosapi.Memo, error) { return &memosapi.Memo{Name: "memos/other"}, nil }); err != nil || memo.Name != "memos/other" {
		t.Fatalf("independent album: %v, %v", memo, err)
	}
	close(release)
	wg.Wait()
	close(results)
	for name := range results {
		if name != "memos/album" {
			t.Fatalf("got %s", name)
		}
	}
	if creations.Load() != 1 {
		t.Fatalf("created %d memos for one album", creations.Load())
	}
}

func TestAlbumRetriesAfterFailedCreation(t *testing.T) {
	groups := newMediaGroups(time.Hour)
	if _, err := groups.memo(context.Background(), "album", func() (*memosapi.Memo, error) { return nil, errors.New("unavailable") }); err == nil {
		t.Fatal("expected failure")
	}
	memo, err := groups.memo(context.Background(), "album", func() (*memosapi.Memo, error) { return &memosapi.Memo{Name: "memos/retry"}, nil })
	if err != nil || memo.Name != "memos/retry" {
		t.Fatalf("retry: %v, %v", memo, err)
	}
}

func TestAlbumWaitHonorsContext(t *testing.T) {
	groups := newMediaGroups(time.Hour)
	release := make(chan struct{})
	defer close(release)
	go func() {
		_, _ = groups.memo(context.Background(), "album", func() (*memosapi.Memo, error) {
			<-release
			return &memosapi.Memo{}, nil
		})
	}()
	for {
		groups.mu.Lock()
		_, started := groups.entries["album"]
		groups.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := groups.memo(ctx, "album", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}

func TestAlbumsExpire(t *testing.T) {
	now := time.Unix(0, 0)
	groups := newMediaGroups(time.Minute)
	groups.now = func() time.Time { return now }
	created := func(name string) func() (*memosapi.Memo, error) {
		return func() (*memosapi.Memo, error) { return &memosapi.Memo{Name: name}, nil }
	}
	if _, err := groups.memo(context.Background(), "album", created("memos/first")); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Minute)
	memo, err := groups.memo(context.Background(), "album", created("memos/second"))
	if err != nil || memo.Name != "memos/second" {
		t.Fatalf("expired album was reused: %v, %v", memo, err)
	}
	if len(groups.entries) != 1 {
		t.Fatalf("expired entries were kept: %d", len(groups.entries))
	}
}
