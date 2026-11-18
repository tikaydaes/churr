package store

import (
	"context"

	"github.com/tikaydaes/churr/internal/model"
)

// Modules is what the built-in modules keep (docs/DESIGN-MODULES.md section
// 6.2): the `module_values` table, each module's keys and bytes per scope.
// The host of the modules checks keys and the size of one value; this area
// keeps the totals.
type Modules interface {
	// Value returns the bytes stored under a key. found is false when
	// there are none.
	Value(ctx context.Context, k model.ModuleKey) (value []byte, found bool, err error)
	// PutValue stores bytes under a key, replacing what was there. It
	// refuses with model.ErrTooLarge, storing nothing, when afterwards the
	// module's values in the key's conversation would come to more than
	// perConversation bytes (conversation scope only), or all the module's
	// values to more than perModule bytes. A limit of 0 is no limit.
	PutValue(ctx context.Context, k model.ModuleKey, value []byte, perConversation, perModule int64) error
	// DeleteValue removes a key. A key with nothing stored is no error.
	DeleteValue(ctx context.Context, k model.ModuleKey) error
}
