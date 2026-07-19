/* 載入設定檔案 */

package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	Port              string
	DatabaseURL       string
	PrivateKeyPath    string
	PublicKeyPath     string
	DockerImage       string
	StoragePath       string
	HostStoragePath   string
	MaxConcurrentJobs int
	TimeLimitSeconds  int
	MaxUploadMB       int
	JudgeMemoryLimit  string // e.g. "512m"; empty disables the limit
	JudgeCPULimit     string // e.g. "1.0";  empty disables the limit
	SeedAdminUsername string // seed admin account on startup; empty disables seeding
	SeedAdminPassword string // plaintext; hashed before insert
}

func Load() *Config {
	_ = godotenv.Load()

	maxJobs, _ := strconv.Atoi(getEnv("MAX_CONCURRENT_JOBS", "3"))
	timeLimit, _ := strconv.Atoi(getEnv("TIME_LIMIT_SECONDS", "10"))
	maxUploadMB, _ := strconv.Atoi(getEnv("MAX_UPLOAD_MB", "64"))

	storagePath := getEnv("STORAGE_PATH", "./storage")

	return &Config{
		Port:              getEnv("PORT", "8080"),
		DatabaseURL:       getEnv("DATABASE_URL", "postgres://regs:password@localhost:5432/regs"),
		PrivateKeyPath:    getEnv("PRIVATE_KEY_PATH", "./keys/private.pem"),
		PublicKeyPath:     getEnv("PUBLIC_KEY_PATH", "./keys/public.pem"),
		DockerImage:       getEnv("DOCKER_IMAGE", "yhlib/cs3060701"),
		StoragePath:       storagePath,
		HostStoragePath:   getEnv("HOST_STORAGE_PATH", storagePath),
		MaxConcurrentJobs: maxJobs,
		TimeLimitSeconds:  timeLimit,
		MaxUploadMB:       maxUploadMB,
		JudgeMemoryLimit:  getEnv("JUDGE_MEMORY_LIMIT", "512m"),
		JudgeCPULimit:     getEnv("JUDGE_CPU_LIMIT", "1.0"),
		SeedAdminUsername: getEnv("SEED_ADMIN_USERNAME", "admin"),
		SeedAdminPassword: getEnv("SEED_ADMIN_PASSWORD", "admin1234"),
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
