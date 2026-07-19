package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"regs/internal/api"
	"regs/internal/api/handler"
	"regs/internal/api/middleware"
	"regs/internal/config"
	"regs/internal/db"
	"regs/internal/judge"
	"regs/internal/model"
	"regs/internal/queue"
	"regs/internal/repository"

	"golang.org/x/crypto/bcrypt"
)

// seedAdmin creates the configured admin account if it does not already exist.
// It is idempotent: existing accounts are left untouched, so restarts are safe.
func seedAdmin(ctx context.Context, userRepo *repository.UserRepository, cfg *config.Config) error {
	if cfg.SeedAdminUsername == "" || cfg.SeedAdminPassword == "" {
		return nil // seeding disabled
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(cfg.SeedAdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	created, err := userRepo.EnsureUser(ctx, cfg.SeedAdminUsername, string(hash), model.RoleAdmin)
	if err != nil {
		return err
	}
	if created {
		log.Printf("seeded admin account %q", cfg.SeedAdminUsername)
	}
	return nil
}

func main() {
	cfg := config.Load()

	// Ensure required storage directories exist.
	for _, dir := range []string{
		fmt.Sprintf("%s/uploads", cfg.StoragePath),
		fmt.Sprintf("%s/workspace", cfg.StoragePath),
		fmt.Sprintf("%s/problems", cfg.StoragePath),
		fmt.Sprintf("%s/logs", cfg.StoragePath),
	} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	// Database
	pool, err := db.NewPool(cfg)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	// JWT keys
	privateKey, err := middleware.LoadPrivateKey(cfg)
	if err != nil {
		log.Fatalf("private key: %v", err)
	}
	publicKey, err := middleware.LoadPublicKey(cfg)
	if err != nil {
		log.Fatalf("public key: %v", err)
	}

	// In-memory denylist for JWTs revoked via logout.
	denylist := middleware.NewTokenDenylist()

	// Repositories
	userRepo := repository.NewUserRepository(pool)
	problemRepo := repository.NewProblemRepository(pool)
	submissionRepo := repository.NewSubmissionRepository(pool)

	// Seed a fixed admin account on startup (idempotent).
	if err := seedAdmin(context.Background(), userRepo, cfg); err != nil {
		log.Fatalf("seed admin: %v", err)
	}

	// Judge engine
	dockerRunner, err := judge.NewDockerRunner(cfg.DockerImage, cfg.JudgeMemoryLimit, cfg.JudgeCPULimit)
	if err != nil {
		log.Fatalf("docker: %v", err)
	}
	jdg := judge.New(dockerRunner, submissionRepo, problemRepo, cfg)

	// Job queue (with concurrency semaphore)
	q := queue.New(cfg.MaxConcurrentJobs, jdg)

	// Handlers
	userHandler := handler.NewUserHandler(userRepo, privateKey, denylist)
	problemHandler := handler.NewProblemHandler(problemRepo, cfg)
	submissionHandler := handler.NewSubmissionHandler(submissionRepo, problemRepo, q, cfg)
	statsHandler := handler.NewStatsHandler(submissionRepo)

	// HTTP server
	router := api.SetupRouter(publicKey, denylist, userHandler, problemHandler, submissionHandler, statsHandler)
	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("REGS server listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("forced shutdown: %v", err)
	}
}
