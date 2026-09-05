package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
)

const healthCheckStatement = `SELECT at FROM healthchecks WHERE id = 1`

type Options struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration
}

type Pool struct {
	pool   *pgxpool.Pool
	logger zerolog.Logger
}

func Connect(ctx context.Context, opts Options, logger zerolog.Logger) (*Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(opts.URL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if opts.MaxConns > 0 {
		poolConfig.MaxConns = opts.MaxConns
	}
	if opts.MinConns > 0 {
		poolConfig.MinConns = opts.MinConns
	}
	if opts.MaxConnLifetime > 0 {
		poolConfig.MaxConnLifetime = opts.MaxConnLifetime
	}
	if opts.MaxConnIdleTime > 0 {
		poolConfig.MaxConnIdleTime = opts.MaxConnIdleTime
	}
	if opts.ConnectTimeout > 0 {
		poolConfig.ConnConfig.ConnectTimeout = opts.ConnectTimeout
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}
	return &Pool{pool: pool, logger: logger}, nil
}

func (p *Pool) Pool() *pgxpool.Pool {
	return p.pool
}

func (p *Pool) Ping(ctx context.Context) error {
	if err := p.pool.Ping(ctx); err != nil {
		return fmt.Errorf("ping database: %w", err)
	}
	var at time.Time
	if err := p.pool.QueryRow(ctx, healthCheckStatement).Scan(&at); err != nil {
		return fmt.Errorf("read healthchecks row: %w", err)
	}
	return nil
}

func (p *Pool) Close() {
	p.pool.Close()
}
