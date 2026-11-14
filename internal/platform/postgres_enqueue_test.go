package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oktanthq/core/internal/queue"
)

type stubTx struct {
	execSQL     []string
	querySQL    []string
	execErr     error
	queryErr    error
	commitErr   error
	rollbackErr error
	committed   bool
	rolledBack  bool
	msgID       int64
}

func (s *stubTx) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	s.execSQL = append(s.execSQL, sql)
	if s.execErr != nil {
		return pgconn.CommandTag{}, s.execErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}

func (s *stubTx) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	s.querySQL = append(s.querySQL, sql)
	return stubRow{err: s.queryErr, id: s.msgID}
}

func (s *stubTx) Commit(context.Context) error {
	s.committed = true
	return s.commitErr
}

func (s *stubTx) Rollback(context.Context) error {
	s.rolledBack = true
	return s.rollbackErr
}

type stubRow struct {
	err error
	id  int64
}

func (r stubRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > 0 {
		if ptr, ok := dest[0].(*int64); ok {
			*ptr = r.id
		}
	}
	return nil
}

func TestEnqueueAnalysisInsertsAndSends(t *testing.T) {
	tx := &stubTx{msgID: 7}
	run := AnalysisRun{
		ID:                 "11111111-1111-1111-1111-111111111111",
		RepositoryID:       "22222222-2222-2222-2222-222222222222",
		AnalysisType:       "security",
		CommitSHA:          "abc123",
		Trigger:            "manual",
		RequestedAnalyzers: []string{"sast"},
		CreatedAt:          time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC),
	}
	msg, _ := json.Marshal(map[string]string{"type": queue.TypeAnalysisRequested})
	if err := enqueueAnalysis(context.Background(), tx, run, msg); err != nil {
		t.Fatalf("enqueueAnalysis: %v", err)
	}
	if len(tx.execSQL) != 1 || !strings.Contains(tx.execSQL[0], "INSERT INTO analysis_runs") {
		t.Fatalf("exec SQL = %v", tx.execSQL)
	}
	if len(tx.querySQL) != 1 || !strings.Contains(tx.querySQL[0], "pgmq.send") {
		t.Fatalf("query SQL = %v", tx.querySQL)
	}
	if !strings.Contains(tx.querySQL[0], "$1") {
		t.Fatalf("expected parameterized send, got %s", tx.querySQL[0])
	}
}

func TestEnqueueAnalysisMapsUniqueViolation(t *testing.T) {
	tx := &stubTx{execErr: &pgconn.PgError{Code: "23505"}}
	err := enqueueAnalysis(context.Background(), tx, AnalysisRun{
		ID: "1", RepositoryID: "2", AnalysisType: "security", CommitSHA: "s", Trigger: "manual",
	}, []byte(`{}`))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v", err)
	}
}

func TestWithTxCommitsOnSuccess(t *testing.T) {
	tx := &stubTx{msgID: 1}
	err := withTx(context.Background(), func(context.Context) (txConn, error) {
		return tx, nil
	}, func(txConn) error {
		return enqueueAnalysis(context.Background(), tx, AnalysisRun{
			ID: "1", RepositoryID: "2", AnalysisType: "security", CommitSHA: "s", Trigger: "manual",
		}, []byte(`{"ok":true}`))
	})
	if err != nil {
		t.Fatalf("withTx: %v", err)
	}
	if !tx.committed {
		t.Fatal("expected commit")
	}
	if !tx.rolledBack {
		t.Fatal("rollback still runs after commit")
	}
}

func TestWithTxRollsBackWhenSendFails(t *testing.T) {
	tx := &stubTx{queryErr: errors.New("pgmq down")}
	err := withTx(context.Background(), func(context.Context) (txConn, error) {
		return tx, nil
	}, func(txConn) error {
		return enqueueAnalysis(context.Background(), tx, AnalysisRun{
			ID: "1", RepositoryID: "2", AnalysisType: "security", CommitSHA: "s", Trigger: "manual",
		}, []byte(`{}`))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if tx.committed {
		t.Fatal("send failure must not commit")
	}
	if !tx.rolledBack {
		t.Fatal("expected rollback")
	}
	if len(tx.execSQL) != 1 {
		t.Fatalf("insert should have run, execSQL = %v", tx.execSQL)
	}
}

func TestWithTxRollsBackWhenInsertFails(t *testing.T) {
	tx := &stubTx{execErr: errors.New("insert failed")}
	err := withTx(context.Background(), func(context.Context) (txConn, error) {
		return tx, nil
	}, func(txConn) error {
		return enqueueAnalysis(context.Background(), tx, AnalysisRun{
			ID: "1", RepositoryID: "2", AnalysisType: "security", CommitSHA: "s", Trigger: "manual",
		}, []byte(`{}`))
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if tx.committed {
		t.Fatal("insert failure must not commit")
	}
	if len(tx.querySQL) != 0 {
		t.Fatal("pgmq.send must not run after insert failure")
	}
}
