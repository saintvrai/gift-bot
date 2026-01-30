package postgres

import (
	"context"
	"errors"
	"math/rand"
	"time"

	"github.com/jmoiron/sqlx"
)

type RetryConfig struct {
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	MaxElapsed  time.Duration
	MaxAttempts int
	PingTimeout time.Duration
	Jitter      float64
}

type LogFunc func(format string, args ...any)

func init() {
	rand.Seed(time.Now().UnixNano())
}

func ConnectWithRetry(ctx context.Context, driver, dsn string, cfg RetryConfig, logf LogFunc) (*sqlx.DB, error) {
	cfg = normalizeConfig(cfg)

	start := time.Now()
	attempt := 0
	backoff := cfg.BaseDelay

	for {
		attempt++
		if cfg.MaxAttempts > 0 && attempt > cfg.MaxAttempts {
			return nil, errors.New("db connect: max attempts reached")
		}
		if cfg.MaxElapsed > 0 && time.Since(start) > cfg.MaxElapsed {
			return nil, errors.New("db connect: max elapsed time reached")
		}

		db, err := sqlx.Open(driver, dsn)
		if err == nil {
			pingCtx := ctx
			cancel := func() {}
			if cfg.PingTimeout > 0 {
				pingCtx, cancel = context.WithTimeout(ctx, cfg.PingTimeout)
			}
			err = db.PingContext(pingCtx)
			cancel()
			if err == nil {
				if logf != nil {
					logf("DB connected after %d attempt(s)", attempt)
				}
				return db, nil
			}
			_ = db.Close()
		}

		if logf != nil {
			logf("DB connect attempt %d failed: %v", attempt, err)
		}

		delay := jitterDelay(backoff, cfg.Jitter)
		if cfg.MaxDelay > 0 && delay > cfg.MaxDelay {
			delay = cfg.MaxDelay
		}
		if backoff < cfg.MaxDelay {
			backoff *= 2
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
}

func normalizeConfig(cfg RetryConfig) RetryConfig {
	if cfg.BaseDelay <= 0 {
		cfg.BaseDelay = time.Second
	}
	if cfg.MaxDelay <= 0 {
		cfg.MaxDelay = 30 * time.Second
	}
	if cfg.PingTimeout <= 0 {
		cfg.PingTimeout = 5 * time.Second
	}
	if cfg.Jitter < 0 {
		cfg.Jitter = 0
	}
	if cfg.Jitter > 0.5 {
		cfg.Jitter = 0.5
	}
	return cfg
}

func jitterDelay(base time.Duration, jitter float64) time.Duration {
	if jitter <= 0 {
		return base
	}
	factor := 1 + (rand.Float64()*2-1)*jitter
	return time.Duration(float64(base) * factor)
}
