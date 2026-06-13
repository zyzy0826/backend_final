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
	"regs/internal/queue"
	"regs/internal/repository"
)

func main() {
	cfg := config.Load()

	// Ensure required storage directories exist.
	for _, dir := range []string{
		fmt.Sprintf("%s/uploads", cfg.StoragePath),
		fmt.Sprintf("%s/workspace", cfg.StoragePath),
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

	// Repositories
	userRepo := repository.NewUserRepository(pool)
	problemRepo := repository.NewProblemRepository(pool)
	submissionRepo := repository.NewSubmissionRepository(pool)

	// Judge engine
	dockerRunner, err := judge.NewDockerRunner(cfg.DockerImage)
	if err != nil {
		log.Fatalf("docker: %v", err)
	}
	jdg := judge.New(dockerRunner, submissionRepo, cfg)

	// Job queue (with concurrency semaphore)
	q := queue.New(cfg.MaxConcurrentJobs, jdg)

	// Handlers
	userHandler := handler.NewUserHandler(userRepo, privateKey)
	problemHandler := handler.NewProblemHandler(problemRepo)
	submissionHandler := handler.NewSubmissionHandler(submissionRepo, problemRepo, q, cfg)
	statsHandler := handler.NewStatsHandler(submissionRepo)

	// HTTP server
	router := api.SetupRouter(publicKey, userHandler, problemHandler, submissionHandler, statsHandler)
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
