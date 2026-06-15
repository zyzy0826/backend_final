package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"regs/internal/model"
)

type UserRepository struct {
	db *pgxpool.Pool
}

func NewUserRepository(db *pgxpool.Pool) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) Create(ctx context.Context, username, passwordHash string) (*model.User, error) {
	// TODO: implement
	return nil, nil
}

func (r *UserRepository) FindByID(ctx context.Context, id int) (*model.User, error) {
	// TODO: implement
	return nil, nil
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	// TODO: implement
	return nil, nil
}
