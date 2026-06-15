package handler

import (
	"crypto/ecdsa"

	"github.com/gin-gonic/gin"
	"regs/internal/model"
	"regs/internal/repository"
)

type UserHandler struct {
	userRepo   *repository.UserRepository
	privateKey *ecdsa.PrivateKey
}

func NewUserHandler(userRepo *repository.UserRepository, privateKey *ecdsa.PrivateKey) *UserHandler {
	return &UserHandler{userRepo: userRepo, privateKey: privateKey}
}

func (h *UserHandler) Register(c *gin.Context) {
	// TODO: implement
	// 1. Bind JSON (username, password)
	// 2. bcrypt hash the password
	// 3. Call userRepo.Create
	// 4. Return 201 with created user
}

func (h *UserHandler) Login(c *gin.Context) {
	// TODO: implement
	// 1. Bind JSON (username, password)
	// 2. Find user by username
	// 3. bcrypt.CompareHashAndPassword
	// 4. Sign a JWT with signToken
	// 5. Return {"token": "..."}
}

func (h *UserHandler) Logout(c *gin.Context) {
	// TODO: implement (JWT is stateless; return 200 with a message)
}

func (h *UserHandler) Me(c *gin.Context) {
	// TODO: implement
	// 1. Get user_id from context (set by Auth middleware)
	// 2. Call userRepo.FindByID
	// 3. Return user
}

func (h *UserHandler) signToken(user *model.User) (string, error) {
	// TODO: implement
	// Build Claims (UserID, Username, Role, ExpiresAt=24h), sign with ES256
	return "", nil
}
