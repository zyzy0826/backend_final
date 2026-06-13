package middleware

import (
	"crypto/ecdsa"
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

// Auth requires a valid JWT in the Authorization header.
func Auth(publicKey *ecdsa.PublicKey) gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := parseToken(c, publicKey)
		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			c.Abort()
			return
		}
		setClaims(c, token)
		c.Next()
	}
}

// OptionalAuth parses the JWT if present, but does not abort if missing.
func OptionalAuth(publicKey *ecdsa.PublicKey) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := parseToken(c, publicKey); err == nil && token.Valid {
			setClaims(c, token)
		}
		c.Next()
	}
}

func parseToken(c *gin.Context, publicKey *ecdsa.PublicKey) (*jwt.Token, error) {
	header := c.GetHeader("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return nil, jwt.ErrTokenMalformed
	}
	tokenStr := strings.TrimPrefix(header, "Bearer ")
	return jwt.ParseWithClaims(tokenStr, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		return publicKey, nil
	}, jwt.WithValidMethods([]string{"ES256"}))
}

func setClaims(c *gin.Context, token *jwt.Token) {
	if claims, ok := token.Claims.(*Claims); ok {
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", string(claims.Role))
	}
}
