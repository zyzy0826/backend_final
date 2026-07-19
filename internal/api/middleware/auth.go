package middleware

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

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
// A token whose jti has been revoked (via logout) is rejected even if its
// signature and expiry are still valid.
func Auth(publicKey *ecdsa.PublicKey, denylist *TokenDenylist) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := parseToken(c, publicKey)
		if err != nil || !token.Valid || isRevoked(token, denylist) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		setClaims(c, token)
		c.Next()
	}
}

// OptionalAuth parses the JWT if present, but does not abort if missing or invalid.
func OptionalAuth(publicKey *ecdsa.PublicKey, denylist *TokenDenylist) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := parseToken(c, publicKey); err == nil && token.Valid && !isRevoked(token, denylist) {
			setClaims(c, token)
		}
		c.Next()
	}
}

// isRevoked reports whether the token's jti is on the denylist.
func isRevoked(token *jwt.Token, denylist *TokenDenylist) bool {
	if denylist == nil {
		return false
	}
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return false
	}
	return denylist.IsRevoked(claims.ID)
}

func parseToken(c *gin.Context, publicKey *ecdsa.PublicKey) (*jwt.Token, error) {
	authHeader := c.GetHeader("Authorization")
	if authHeader == "" {
		return nil, errors.New("missing authorization header")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
		return nil, errors.New("invalid authorization header format")
	}

	return jwt.ParseWithClaims(parts[1], &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return publicKey, nil
	})
}

func setClaims(c *gin.Context, token *jwt.Token) {
	claims, ok := token.Claims.(*Claims)
	if !ok {
		return
	}
	c.Set("user_id", claims.UserID)
	c.Set("username", claims.Username)
	c.Set("role", claims.Role)
	c.Set("jti", claims.ID)
	if claims.ExpiresAt != nil {
		c.Set("token_exp", claims.ExpiresAt.Time)
	}
}
