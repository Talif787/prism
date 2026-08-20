package pgstore

import "github.com/jackc/pgx/v5/pgconn"

// pgconnCommandTag aliases the pgx command tag so the querier interface in this
// package does not force callers to import pgconn directly.
type pgconnCommandTag = pgconn.CommandTag
