package http

import (
	"context"
	"database/sql/driver"
	"fmt"
	"io"
	"math"
	"sort"
)

type result struct {
	id      int64
	changes int64
}

func (r *result) LastInsertId() (int64, error) {
	return r.id, nil
}

func (r *result) RowsAffected() (int64, error) {
	return r.changes, nil
}

type rows struct {
	result        *resultSet
	currentRowIdx int
}

func (r *rows) Columns() []string {
	return r.result.Columns
}

func (r *rows) Close() error {
	return nil
}

func (r *rows) Next(dest []driver.Value) error {
	if r.currentRowIdx == len(r.result.Rows) {
		return io.EOF
	}
	count := len(r.result.Rows[r.currentRowIdx])
	for idx := 0; idx < count; idx++ {
		value := r.result.Rows[r.currentRowIdx][idx]
		dest[idx] = value
		switch v := value.(type) {
		case int64:
			dest[idx] = int64(v)
		case float64:
			if math.Mod(v, 1) >= 0 {
				dest[idx] = int64(v)
			} else {
				dest[idx] = v
			}
		default:
			dest[idx] = value
		}
	}
	r.currentRowIdx++
	return nil
}

type conn struct {
	url string
	jwt string
}

func Connect(url, jwt string) *conn {
	return &conn{url, jwt}
}

func (c *conn) Prepare(query string) (driver.Stmt, error) {
	return nil, fmt.Errorf("prepare method not implemented")
}

func (c *conn) Close() error {
	return nil
}

func (c *conn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("begin method not implemented")
}

func convertArgs(args []driver.NamedValue) params {
	if len(args) == 0 {
		return params{}
	}
	var sortedArgs []*driver.NamedValue
	for idx := range args {
		sortedArgs = append(sortedArgs, &args[idx])
	}
	sort.Slice(sortedArgs, func(i, j int) bool {
		return sortedArgs[i].Ordinal < sortedArgs[j].Ordinal
	})
	var names []string
	var values []any
	for idx := range sortedArgs {
		if len(sortedArgs[idx].Name) > 0 {
			names = append(names, sortedArgs[idx].Name)
		}
		values = append(values, sortedArgs[idx].Value)
	}
	return params{Names: names, Values: values}
}

func (c *conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	_, err := callSqld(ctx, c.url, c.jwt, query, convertArgs(args))
	if err != nil {
		return nil, err
	}
	return &result{0, 0}, nil
}

func (c *conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	rs, err := callSqld(ctx, c.url, c.jwt, query, convertArgs(args))
	if err != nil {
		return nil, err
	}
	return &rows{rs, 0}, nil
}
