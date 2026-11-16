// Package txn runs a function in one ent transaction: the domain write, its activity entry and its
// outbox event commit together or not at all.
package txn

import (
	"context"
	"fmt"

	"github.com/bengobox/projects-service/internal/ent"
)

// Run calls fn inside a transaction, committing when it returns nil and rolling back otherwise
// (also when fn panics).
func Run(ctx context.Context, client *ent.Client, fn func(tx *ent.Tx) error) (err error) {
	tx, err := client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
