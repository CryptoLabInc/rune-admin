package server

import (
	"context"

	"github.com/CryptoLabInc/rune-console/internal/crypto"
)

// consoleEngine is the runespace engine surface the handlers depend on. It exists
// so the gRPC handlers can be unit-tested with a fake (observing the recall
// scope passed to Search — the fail-OPEN sentinel regression, plan §0/§6-D5)
// without a live runespace. *crypto.Engine satisfies it as written (keys.go),
// so production wiring in daemon.go is unchanged.
type consoleEngine interface {
	InsertPreEncrypted(ctx context.Context, it crypto.PreEncryptedItem, filterTags ...string) error
	Centroids(ctx context.Context) (*crypto.CentroidSet, error)
	Search(ctx context.Context, vec []float32, topK int, filterScope ...string) ([]crypto.SearchHit, error)
	GetTagStats(ctx context.Context, tags []string) ([]crypto.TagStat, error)
	PurgeTag(ctx context.Context, tag string) (crypto.PurgeResult, error)
	// RemoveTag strips a tag from every item (console team-delete "purge");
	// RetagAll reassigns items from one tag to another (team-delete
	// "transfer"). Both are shipped SDK calls, distinct from the stubbed
	// PurgeTag/GetTagStats above.
	RemoveTag(ctx context.Context, tag string) (uint64, error)
	RetagAll(ctx context.Context, from, to string) (uint64, error)
	Close() error
}
