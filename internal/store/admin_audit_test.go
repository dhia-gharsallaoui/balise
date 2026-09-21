package store_test

import (
	"context"
	"testing"

	"github.com/dhia/balise/internal/store"
	"github.com/dhia/balise/internal/testutil"
	"github.com/stretchr/testify/require"
)

func TestRecordAdminAuditWritesAQueryableRow(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	err := store.RecordAdminAudit(ctx, pool, "agent.create", "new-agent-id",
		map[string]any{"scopes": []string{"work"}, "capabilities": []string{"read"}})
	require.NoError(t, err)

	entries, err := store.ListAdminAudit(ctx, pool, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "owner", entries[0].Actor)
	require.Equal(t, "agent.create", entries[0].Action)
	require.Equal(t, "new-agent-id", entries[0].Target)
	require.Equal(t, []any{"work"}, entries[0].Detail["scopes"])
}

func TestRecordAdminAuditAllowsNilDetail(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	require.NoError(t, store.RecordAdminAudit(ctx, pool, "scope.create", "sandbox", nil))

	entries, err := store.ListAdminAudit(ctx, pool, 0)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Empty(t, entries[0].Detail)
}

func TestListAdminAuditReturnsNewestFirstAndRespectsLimit(t *testing.T) {
	pool := testutil.NewDB(t)
	ctx := context.Background()

	for _, action := range []string{"agent.create", "agent.revoke", "scope.create"} {
		require.NoError(t, store.RecordAdminAudit(ctx, pool, action, "x", nil))
	}

	entries, err := store.ListAdminAudit(ctx, pool, 2)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	require.Equal(t, "scope.create", entries[0].Action)
	require.Equal(t, "agent.revoke", entries[1].Action)
}
