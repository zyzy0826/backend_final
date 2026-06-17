/* 拿到資料庫的網址 → 建立連線池 → ping 一下確認連得上 → 回傳連線物件。
   之後所有 repository 都透過這個 pool 來跟資料庫溝通。 */

package db

import (
	"context"
	"fmt"

	"regs/internal/config"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(cfg *config.Config) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return pool, nil
}
