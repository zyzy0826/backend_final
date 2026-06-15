package middleware

import (
	"crypto/ecdsa"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"regs/internal/config"
	"regs/internal/model"
)

type Claims struct {
	UserID   int        `json:"user_id"`
	Username string     `json:"username"`
	Role     model.Role `json:"role"`
	jwt.RegisteredClaims
}

func LoadPrivateKey(cfg *config.Config) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(cfg.PrivateKeyPath)
	if err != nil {
		return nil, err
	}
	return jwt.ParseECPrivateKeyFromPEM(data)
}

func LoadPublicKey(cfg *config.Config) (*ecdsa.PublicKey, error) {
	data, err := os.ReadFile(cfg.PublicKeyPath)
	if err != nil {
		return nil, err
	}
	return jwt.ParseECPublicKeyFromPEM(data)
}

// Auth requires a valid JWT in the Authorization: Bearer <token> header.
func Auth(publicKey *ecdsa.PublicKey) gin.HandlerFunc {
	// TODO: implement — parse token, abort with 401 if invalid, set claims into context
	return func(c *gin.Context) {
		c.Next()
	}
}

// OptionalAuth parses the JWT if present, but does not abort if missing or invalid.
func OptionalAuth(publicKey *ecdsa.PublicKey) gin.HandlerFunc {
	// TODO: implement — if token present and valid, set claims into context; always call Next()
	return func(c *gin.Context) {
		c.Next()
	}
}

func parseToken(c *gin.Context, publicKey *ecdsa.PublicKey) (*jwt.Token, error) {
	// TODO: implement — extract Bearer token from Authorization header, parse with ES256
	return nil, nil
}

func setClaims(c *gin.Context, token *jwt.Token) {
	// TODO: implement — set user_id, username, role into gin context
}
