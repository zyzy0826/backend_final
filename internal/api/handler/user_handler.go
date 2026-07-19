package handler

import (
	"crypto/ecdsa"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"regs/internal/api/middleware"
	"regs/internal/model"
	"regs/internal/repository"
)

type UserHandler struct {
	userRepo   *repository.UserRepository
	privateKey *ecdsa.PrivateKey
	denylist   *middleware.TokenDenylist
}

func NewUserHandler(userRepo *repository.UserRepository, privateKey *ecdsa.PrivateKey, denylist *middleware.TokenDenylist) *UserHandler {
	return &UserHandler{userRepo: userRepo, privateKey: privateKey, denylist: denylist}
}

type credentials struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

func (h *UserHandler) Register(c *gin.Context) {
	var req credentials
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to hash password"})
		return
	}

	user, err := h.userRepo.Create(c.Request.Context(), req.Username, string(hash))
	if err != nil {
		// Most likely a duplicate username (UNIQUE constraint).
		c.JSON(http.StatusConflict, gin.H{"error": "username already taken"})
		return
	}

	c.JSON(http.StatusCreated, user)
}

func (h *UserHandler) Login(c *gin.Context) {
	var req credentials
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	user, err := h.userRepo.FindByUsername(c.Request.Context(), req.Username)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid credentials"})
		return
	}

	token, err := h.signToken(user)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to sign token"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"token": token})
}

func (h *UserHandler) Logout(c *gin.Context) {
	// JWTs are stateless, so we revoke this token's id server-side until it would
	// have expired. Subsequent requests carrying it are rejected by Auth.
	jti := c.GetString("jti")
	exp := c.GetTime("token_exp")
	if exp.IsZero() {
		// Fallback: a token without an expiry claim; keep it revoked for the
		// maximum token lifetime so it cannot outlive a normal one.
		exp = time.Now().Add(24 * time.Hour)
	}
	h.denylist.Revoke(jti, exp)

	c.JSON(http.StatusOK, gin.H{"message": "logged out"})
}

func (h *UserHandler) Me(c *gin.Context) {
	userID := c.GetInt("user_id")

	user, err := h.userRepo.FindByID(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "user not found"})
		return
	}
	c.JSON(http.StatusOK, user)
}

func (h *UserHandler) signToken(user *model.User) (string, error) {
	claims := middleware.Claims{
		UserID:   user.ID,
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        uuid.NewString(), // jti — enables per-token revocation on logout
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	return token.SignedString(h.privateKey)
}
