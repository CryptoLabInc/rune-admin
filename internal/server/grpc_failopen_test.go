package server

import (
	"context"
	"testing"

	"github.com/CryptoLabInc/rune-console/internal/crypto"
	"github.com/CryptoLabInc/rune-console/internal/tokens"
	pb "github.com/CryptoLabInc/rune-console/pkg/consolepb"
)

// fakeEngine records the filter scope handed to Search so the fail-OPEN
// regression can assert what a membership-0 caller is narrowed to. It stands in
// for *crypto.Engine (consoleEngine) so the test needs no live runespace.
type fakeEngine struct {
	called   bool
	gotScope []string

	// InsertPreEncrypted observation.
	insertCalled  bool
	gotItem       crypto.PreEncryptedItem
	gotInsertTags []string
	insertErr     error

	// PurgeTag observation/knobs (group-delete tag cleanup tests).
	purgedTag string
	purgeRes  crypto.PurgeResult
	purgeErr  error

	// RemoveTag / RetagAll observation/knobs (console team-delete memory action).
	removedTag  string
	removeCount uint64
	removeErr   error
	retagFrom   string
	retagTo     string
	retagCount  uint64
	retagErr    error
}

func (f *fakeEngine) InsertPreEncrypted(_ context.Context, it crypto.PreEncryptedItem, filterTags ...string) error {
	f.insertCalled = true
	f.gotItem = it
	f.gotInsertTags = filterTags
	return f.insertErr
}

func (f *fakeEngine) Centroids(_ context.Context) (*crypto.CentroidSet, error) {
	return &crypto.CentroidSet{}, nil
}

func (f *fakeEngine) Search(_ context.Context, _ []float32, _ int, filterScope ...string) ([]crypto.SearchHit, error) {
	f.called = true
	f.gotScope = filterScope
	return nil, nil
}

func (f *fakeEngine) GetTagStats(_ context.Context, _ []string) ([]crypto.TagStat, error) {
	return nil, nil
}

func (f *fakeEngine) PurgeTag(_ context.Context, tag string) (crypto.PurgeResult, error) {
	f.purgedTag = tag
	return f.purgeRes, f.purgeErr
}

func (f *fakeEngine) RemoveTag(_ context.Context, tag string) (uint64, error) {
	f.removedTag = tag
	return f.removeCount, f.removeErr
}

func (f *fakeEngine) RetagAll(_ context.Context, from, to string) (uint64, error) {
	f.retagFrom, f.retagTo = from, to
	return f.retagCount, f.retagErr
}

func (f *fakeEngine) Close() error { return nil }

// TestSearchFailOpenNoMembership is the plan §0 regression. A valid token whose
// user has no group memberships must be narrowed to public-only — it must NOT
// be handed an empty scope, which runespace reads as "filtering off" and would
// return the whole org for the console to decrypt. We inject a fake engine and
// assert the recall scope it receives is the sentinel, not empty.
func TestSearchFailOpenNoMembership(t *testing.T) {
	v := newTestConsole(t)
	fake := &fakeEngine{}
	v.engine = fake // same-package test replaces the nil engine with the fake
	srv := NewConsoleGRPC(v)

	// DemoToken's user has no group membership, so RecallScope is empty —
	// exactly the fail-OPEN trigger.
	_, err := srv.Search(context.Background(), &pb.SearchRequest{
		Token:  tokens.DemoToken,
		Vector: []float32{0.1, 0.2},
		TopK:   5,
	})
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if !fake.called {
		t.Fatal("engine.Search was not called")
	}
	if len(fake.gotScope) != 1 || fake.gotScope[0] != publicOnlyScopeSentinel {
		t.Fatalf("membership-0 recall scope = %v, want [%q]; an empty scope disables filtering and leaks the org",
			fake.gotScope, publicOnlyScopeSentinel)
	}
}
