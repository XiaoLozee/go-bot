package media

import "context"

type Store interface {
	Close() error
	UpsertAsset(ctx context.Context, item Asset) error
}
