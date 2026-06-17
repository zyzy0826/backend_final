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
}

func Load() *Config {
	_ = godotenv.Load()

	maxJobs, _ := strconv.Atoi(getEnv("MAX_CONCURRENT_JOBS", "3"))
	timeLimit, _ := strconv.Atoi(getEnv("TIME_LIMIT_SECONDS", "10"))

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
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
