package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ListenAddr        string
	DataDir           string
	ProxyAPIKey       string
	DebugLogPayloads  bool
	DefaultModel      string
	CodexBaseURL      string
	AuthIssuer        string
	OAuthClientID     string
	LoginTimeout      time.Duration
	ContinuationTTL   time.Duration
	RequestTimeout    time.Duration
	RefreshSkew       time.Duration
	TokenOptimization TokenOptimizationConfig
}

// TokenOptimizationConfig controls optional, semantics-preserving prompt
// optimization. A remote compressor is deliberately optional: requests keep
// working unchanged when it is unavailable.
type TokenOptimizationConfig struct {
	Enabled              bool
	CompressorURL        string
	CompressorTimeout    time.Duration
	MinRequestTokens     int
	MinTextTokens        int
	TargetRatio          float64
	CacheEntries         int
	AutoCompactThreshold int
}

func Load() (Config, error) {
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		return Config{}, fmt.Errorf("load .env: %w", err)
	}

	dataDir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dataDir == "" {
		dataDir = "data"
	}
	if !filepath.IsAbs(dataDir) {
		cwd, err := os.Getwd()
		if err != nil {
			return Config{}, fmt.Errorf("resolve cwd: %w", err)
		}
		dataDir = filepath.Join(cwd, dataDir)
	}

	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber <= 0 || portNumber > 65535 {
		return Config{}, fmt.Errorf("PORT must be a valid TCP port")
	}
	debugLogPayloads := false
	if raw := strings.TrimSpace(os.Getenv("DEBUG_LOG_PAYLOADS")); raw != "" {
		var err error
		debugLogPayloads, err = strconv.ParseBool(raw)
		if err != nil {
			return Config{}, fmt.Errorf("DEBUG_LOG_PAYLOADS must be a boolean")
		}
	}

	cfg := Config{
		ListenAddr:       ":" + strconv.Itoa(portNumber),
		DataDir:          dataDir,
		ProxyAPIKey:      strings.TrimSpace(os.Getenv("PROXY_API_KEY")),
		DebugLogPayloads: debugLogPayloads,
		DefaultModel:     "gpt-5.6-sol",
		CodexBaseURL:     "https://chatgpt.com/backend-api",
		AuthIssuer:       "https://auth.openai.com",
		OAuthClientID:    "app_EMoamEEZ73f0CkXaXp7hrann",
		LoginTimeout:     15 * time.Minute,
		ContinuationTTL:  time.Hour,
		RequestTimeout:   30 * time.Minute,
		RefreshSkew:      time.Minute,
		TokenOptimization: TokenOptimizationConfig{
			CompressorTimeout: 3 * time.Second,
			MinRequestTokens:  6000,
			MinTextTokens:     800,
			TargetRatio:       0.5,
			CacheEntries:      256,
		},
	}

	if cfg.TokenOptimization.Enabled, err = envBool("TOKEN_OPTIMIZATION_ENABLED", false); err != nil {
		return Config{}, err
	}
	cfg.TokenOptimization.CompressorURL = strings.TrimRight(strings.TrimSpace(os.Getenv("TOKEN_COMPRESSOR_URL")), "/")
	if cfg.TokenOptimization.CompressorTimeout, err = envDuration("TOKEN_COMPRESSOR_TIMEOUT", cfg.TokenOptimization.CompressorTimeout); err != nil {
		return Config{}, err
	}
	if cfg.TokenOptimization.MinRequestTokens, err = envInt("TOKEN_COMPRESSION_MIN_REQUEST_TOKENS", cfg.TokenOptimization.MinRequestTokens, 0); err != nil {
		return Config{}, err
	}
	if cfg.TokenOptimization.MinTextTokens, err = envInt("TOKEN_COMPRESSION_MIN_TEXT_TOKENS", cfg.TokenOptimization.MinTextTokens, 1); err != nil {
		return Config{}, err
	}
	if cfg.TokenOptimization.TargetRatio, err = envFloat("TOKEN_COMPRESSION_TARGET_RATIO", cfg.TokenOptimization.TargetRatio, 0.05, 1); err != nil {
		return Config{}, err
	}
	if cfg.TokenOptimization.CacheEntries, err = envInt("TOKEN_COMPRESSION_CACHE_ENTRIES", cfg.TokenOptimization.CacheEntries, 0); err != nil {
		return Config{}, err
	}
	if cfg.TokenOptimization.AutoCompactThreshold, err = envInt("TOKEN_AUTO_COMPACT_THRESHOLD", 0, 0); err != nil {
		return Config{}, err
	}

	if cfg.ProxyAPIKey == "" {
		return Config{}, fmt.Errorf("PROXY_API_KEY must be set")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return Config{}, fmt.Errorf("create data dir: %w", err)
	}

	return cfg, nil
}

func envBool(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return false, fmt.Errorf("%s must be a boolean", name)
	}
	return value, nil
}

func envDuration(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return value, nil
}

func envInt(name string, fallback, minimum int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minimum {
		return 0, fmt.Errorf("%s must be an integer greater than or equal to %d", name, minimum)
	}
	return value, nil
}

func envFloat(name string, fallback, minimum, maximum float64) (float64, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value < minimum || value > maximum {
		return 0, fmt.Errorf("%s must be between %.2f and %.2f", name, minimum, maximum)
	}
	return value, nil
}
