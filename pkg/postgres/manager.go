package postgres

import (
	"context"
	"sync"
	"time"

	"gift-bot/pkg/config"
	"github.com/jmoiron/sqlx"
)

type Manager struct {
	mu     sync.RWMutex
	db     *sqlx.DB
	driver string
	dsn    string
	cfg    RetryConfig
	logf   LogFunc
}

func NewManager(ctx context.Context, cfg RetryConfig, logf LogFunc) (*Manager, error) {
	dsn := BuildDSN(config.GlobalСonfig.DB)
	return NewManagerWithDSN(ctx, "postgres", dsn, cfg, logf)
}

func NewManagerWithDSN(ctx context.Context, driver, dsn string, cfg RetryConfig, logf LogFunc) (*Manager, error) {
	db, err := ConnectWithRetry(ctx, driver, dsn, cfg, logf)
	if err != nil {
		return nil, err
	}
	return &Manager{
		db:     db,
		driver: driver,
		dsn:    dsn,
		cfg:    normalizeConfig(cfg),
		logf:   logf,
	}, nil
}

func (m *Manager) DB() *sqlx.DB {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.db
}

func (m *Manager) Reconnect(ctx context.Context) error {
	db, err := ConnectWithRetry(ctx, m.driver, m.dsn, m.cfg, m.logf)
	if err != nil {
		return err
	}

	m.mu.Lock()
	old := m.db
	m.db = db
	m.mu.Unlock()

	if old != nil {
		_ = old.Close()
	}
	return nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	db := m.db
	m.db = nil
	m.mu.Unlock()
	if db != nil {
		return db.Close()
	}
	return nil
}

func (m *Manager) MonitorAndReconnect(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			db := m.DB()
			if db == nil {
				_ = m.Reconnect(ctx)
				continue
			}
			pingCtx := ctx
			cancel := func() {}
			if m.cfg.PingTimeout > 0 {
				pingCtx, cancel = context.WithTimeout(ctx, m.cfg.PingTimeout)
			}
			err := db.PingContext(pingCtx)
			cancel()
			if err != nil {
				if m.logf != nil {
					m.logf("DB ping failed: %v", err)
				}
				if err := m.Reconnect(ctx); err != nil && m.logf != nil {
					m.logf("DB reconnect failed: %v", err)
				}
			}
		}
	}
}
