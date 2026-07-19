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
	const q = `
		INSERT INTO users (username, password)
		VALUES ($1, $2)
		RETURNING id, username, password, role, created_at`

	var u model.User
	err := r.db.QueryRow(ctx, q, username, passwordHash).
		Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *UserRepository) FindByID(ctx context.Context, id int) (*model.User, error) {
	const q = `SELECT id, username, password, role, created_at FROM users WHERE id = $1`

	var u model.User
	err := r.db.QueryRow(ctx, q, id).
		Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// EnsureUser inserts a user with the given role only if the username is not
// already taken. It is idempotent — on a duplicate username it does nothing and
// returns created=false. Used to seed fixed accounts (e.g. admin) on startup.
func (r *UserRepository) EnsureUser(ctx context.Context, username, passwordHash string, role model.Role) (created bool, err error) {
	const q = `
		INSERT INTO users (username, password, role)
		VALUES ($1, $2, $3)
		ON CONFLICT (username) DO NOTHING`

	tag, err := r.db.Exec(ctx, q, username, passwordHash, role)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (r *UserRepository) FindByUsername(ctx context.Context, username string) (*model.User, error) {
	const q = `SELECT id, username, password, role, created_at FROM users WHERE username = $1`

	var u model.User
	err := r.db.QueryRow(ctx, q, username).
		Scan(&u.ID, &u.Username, &u.Password, &u.Role, &u.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}
