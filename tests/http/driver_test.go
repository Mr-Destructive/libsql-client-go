package http

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"runtime/debug"
	"testing"
	"time"

	_ "github.com/libsql/libsql-client-go/libsql"
)

type T struct {
	*testing.T
}

func (t T) FatalWithMsg(msg string) {
	t.Log(string(debug.Stack()))
	t.Fatal(msg)
}

func (t T) FatalOnError(err error) {
	if err != nil {
		t.Log(string(debug.Stack()))
		t.Fatal(err)
	}
}

type Database struct {
	*sql.DB
	t   T
	ctx context.Context
}

func getDb(t T) Database {
	dbURL := os.Getenv("LIBSQL_TEST_DB_URL")
	db, err := sql.Open("libsql", dbURL)
	t.FatalOnError(err)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(func() {
		db.Close()
		cancel()
	})

	return Database{db, t, ctx}
}

func (db Database) exec(sql string, args ...any) sql.Result {
	res, err := db.ExecContext(db.ctx, sql, args...)
	db.t.FatalOnError(err)
	return res
}

func (db Database) query(sql string, args ...any) *sql.Rows {
	rows, err := db.QueryContext(db.ctx, sql, args...)
	db.t.FatalOnError(err)
	return rows
}

type Table struct {
	name string
	db   Database
}

func (db Database) createTable() Table {
	name := "test_" + fmt.Sprint(rand.Int()) + "_" + time.Now().Format("20060102150405")
	db.exec("CREATE TABLE " + name + " (a int, b text)")
	db.t.Cleanup(func() {
		db.exec("DROP TABLE " + name)
	})
	return Table{name, db}
}

func (t Table) insertRows(start, count int) {
	t.insertRowsInternal(start, count, func(i int) sql.Result {
		return t.db.exec("INSERT INTO " + t.name + " (a, b) VALUES (" + fmt.Sprint(i) + ", '" + fmt.Sprint(i) + "')")
	})
}

func (t Table) insertRowsWithArgs(start, count int) {
	t.insertRowsInternal(start, count, func(i int) sql.Result {
		return t.db.exec("INSERT INTO "+t.name+" (a, b) VALUES (?, ?)", i, fmt.Sprint(i))
	})
}

func (t Table) insertRowsInternal(start, count int, execFn func(i int) sql.Result) {
	for i := 0; i < count; i++ {
		res := execFn(i + start)
		affected, err := res.RowsAffected()
		t.db.t.FatalOnError(err)
		if affected != 1 {
			t.db.t.FatalWithMsg("expected 1 row affected")
		}
	}
}

func (t Table) assertRowsCount(count int) {
	t.assertCount(count, func() *sql.Rows {
		return t.db.query("SELECT COUNT(*) FROM " + t.name)
	})
}

func (t Table) assertRowDoesNotExist(id int) {
	t.assertCount(0, func() *sql.Rows {
		return t.db.query("SELECT COUNT(*) FROM "+t.name+" WHERE a = ?", id)
	})
}

func (t Table) assertRowExists(id int) {
	t.assertCount(1, func() *sql.Rows {
		return t.db.query("SELECT COUNT(*) FROM "+t.name+" WHERE a = ?", id)
	})
}

func (t Table) assertCount(expectedCount int, queryFn func() *sql.Rows) {
	rows := queryFn()
	defer rows.Close()
	if !rows.Next() {
		t.db.t.FatalWithMsg("expected at least one row")
	}
	var rowCount int
	t.db.t.FatalOnError(rows.Scan(&rowCount))
	if rowCount != expectedCount {
		t.db.t.FatalWithMsg(fmt.Sprintf("expected %d rows, got %d", expectedCount, rowCount))
	}
}

func (t Table) beginTx() Tx {
	tx, err := t.db.BeginTx(t.db.ctx, nil)
	t.db.t.FatalOnError(err)
	return Tx{tx, t, nil}
}

func (t Table) beginTxWithContext(ctx context.Context) Tx {
	tx, err := t.db.BeginTx(ctx, nil)
	t.db.t.FatalOnError(err)
	return Tx{tx, t, &ctx}
}

func (t Table) prepareInsertStmt() PreparedStmt {
	stmt, err := t.db.Prepare("INSERT INTO " + t.name + " (a, b) VALUES (?, ?)")
	t.db.t.FatalOnError(err)
	return PreparedStmt{stmt, t}
}

type PreparedStmt struct {
	*sql.Stmt
	t Table
}

func (s PreparedStmt) exec(args ...any) sql.Result {
	res, err := s.ExecContext(s.t.db.ctx, args...)
	s.t.db.t.FatalOnError(err)
	return res
}

type Tx struct {
	*sql.Tx
	t   Table
	ctx *context.Context
}

func (t Tx) context() context.Context {
	if t.ctx != nil {
		return *t.ctx
	}
	return t.t.db.ctx
}

func (t Tx) exec(sql string, args ...any) sql.Result {
	res, err := t.ExecContext(t.context(), sql, args...)
	t.t.db.t.FatalOnError(err)
	return res
}

func (t Tx) query(sql string, args ...any) *sql.Rows {
	rows, err := t.QueryContext(t.context(), sql, args...)
	t.t.db.t.FatalOnError(err)
	return rows
}

func (t Tx) insertRows(start, count int) {
	t.t.insertRowsInternal(start, count, func(i int) sql.Result {
		return t.exec("INSERT INTO " + t.t.name + " (a, b) VALUES (" + fmt.Sprint(i) + ", '" + fmt.Sprint(i) + "')")
	})
}

func (t Tx) insertRowsWithArgs(start, count int) {
	t.t.insertRowsInternal(start, count, func(i int) sql.Result {
		return t.exec("INSERT INTO "+t.t.name+" (a, b) VALUES (?, ?)", i, fmt.Sprint(i))
	})
}

func (t Tx) assertRowsCount(count int) {
	t.t.assertCount(count, func() *sql.Rows {
		return t.query("SELECT COUNT(*) FROM " + t.t.name)
	})
}

func (t Tx) assertRowDoesNotExist(id int) {
	t.t.assertCount(0, func() *sql.Rows {
		return t.query("SELECT COUNT(*) FROM "+t.t.name+" WHERE a = ?", id)
	})
}

func (t Tx) assertRowExists(id int) {
	t.t.assertCount(1, func() *sql.Rows {
		return t.query("SELECT COUNT(*) FROM "+t.t.name+" WHERE a = ?", id)
	})
}

func (t Tx) prepareInsertStmt() PreparedStmt {
	stmt, err := t.Prepare("INSERT INTO " + t.t.name + " (a, b) VALUES (?, ?)")
	t.t.db.t.FatalOnError(err)
	return PreparedStmt{stmt, t.t}
}

func TestExecAndQuery(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	table.insertRows(0, 10)
	table.insertRowsWithArgs(10, 10)
	table.assertRowsCount(20)
	table.assertRowDoesNotExist(20)
	table.assertRowExists(0)
	table.assertRowExists(19)
}

func TestPreparedStatements(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	stmt := table.prepareInsertStmt()
	stmt.exec(1, "1")
	db.t.FatalOnError(stmt.Close())
	table.assertRowsCount(1)
	table.assertRowExists(1)
}

func TestTransaction(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	tx := table.beginTx()
	tx.insertRows(0, 10)
	tx.insertRowsWithArgs(10, 10)
	tx.assertRowsCount(20)
	tx.assertRowDoesNotExist(20)
	tx.assertRowExists(0)
	tx.assertRowExists(19)
	db.t.FatalOnError(tx.Commit())
	table.assertRowsCount(20)
	table.assertRowDoesNotExist(20)
	table.assertRowExists(0)
	table.assertRowExists(19)
}

func TestPreparedStatementInTransaction(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	tx := table.beginTx()
	stmt := tx.prepareInsertStmt()
	stmt.exec(1, "1")
	db.t.FatalOnError(stmt.Close())
	tx.assertRowsCount(1)
	tx.assertRowExists(1)
	db.t.FatalOnError(tx.Commit())
	table.assertRowsCount(1)
	table.assertRowExists(1)
}

func TestPreparedStatementInTransactionRollback(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	tx := table.beginTx()
	stmt := tx.prepareInsertStmt()
	stmt.exec(1, "1")
	db.t.FatalOnError(stmt.Close())
	tx.assertRowsCount(1)
	tx.assertRowExists(1)
	db.t.FatalOnError(tx.Rollback())
	table.assertRowsCount(0)
	table.assertRowDoesNotExist(1)
}

func TestTransactionRollback(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	tx := table.beginTx()
	tx.insertRows(0, 10)
	tx.insertRowsWithArgs(10, 10)
	tx.assertRowsCount(20)
	tx.assertRowDoesNotExist(20)
	tx.assertRowExists(0)
	tx.assertRowExists(19)
	db.t.FatalOnError(tx.Rollback())
	table.assertRowsCount(0)
}

func TestCancelContext(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := db.ExecContext(ctx, "CREATE TABLE IF NOT EXISTS test (id INTEGER PRIMARY KEY, name TEXT)")
	if err == nil {
		db.t.FatalWithMsg("should have failed")
	}
	if !errors.Is(err, context.Canceled) {
		db.t.FatalWithMsg("should have failed with context.Canceled")
	}
}

func TestCancelContextWithTransaction(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	table := db.createTable()
	ctx, cancel := context.WithCancel(context.Background())
	tx := table.beginTxWithContext(ctx)
	tx.insertRows(0, 10)
	tx.insertRowsWithArgs(10, 10)
	tx.assertRowsCount(20)
	tx.assertRowDoesNotExist(20)
	tx.assertRowExists(0)
	tx.assertRowExists(19)
	// let's cancel the context before the commit
	cancel()
	err := tx.Commit()
	if err == nil {
		db.t.FatalWithMsg("should have failed")
	}
	if !errors.Is(err, context.Canceled) {
		db.t.FatalWithMsg("should have failed with context.Canceled")
	}
	// rolling back the transaction should not result in any error
	db.t.FatalOnError(tx.Rollback())
}

func TestDataTypes(t *testing.T) {
	t.Parallel()
	db := getDb(T{t})
	var (
		text        string
		nullText    sql.NullString
		integer     sql.NullInt64
		nullInteger sql.NullInt64
		boolean     bool
		float8      float64
		nullFloat   sql.NullFloat64
		bytea       []byte
	)
	db.t.FatalOnError(db.QueryRowContext(db.ctx, "SELECT 'foobar' as text, NULL as text,  NULL as integer, 42 as integer, 1 as boolean, X'000102' as bytea, 3.14 as float8, NULL as float8;").Scan(&text, &nullText, &nullInteger, &integer, &boolean, &bytea, &float8, &nullFloat))
	switch {
	case text != "foobar":
		t.Error("value mismatch - text")
	case nullText.Valid:
		t.Error("null text is valid")
	case nullInteger.Valid:
		t.Error("null integer is valid")
	case !integer.Valid:
		t.Error("integer is not valid")
	case integer.Int64 != 42:
		t.Error("value mismatch - integer")
	case !boolean:
		t.Error("value mismatch - boolean")
	case float8 != 3.14:
		t.Error("value mismatch - float8")
	case !bytes.Equal(bytea, []byte{0, 1, 2}):
		t.Error("value mismatch - bytea")
	case nullFloat.Valid:
		t.Error("null float is valid")
	}
}