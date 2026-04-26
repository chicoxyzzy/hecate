// Package bootstrap manages the gateway's first-run secrets: the
// control-plane encryption key and the admin bearer token. Both are
// auto-generated on first start and persisted to a JSON file under the
// configured data directory so subsequent restarts reuse them. The same
// file is also the canonical source operators read when they need to
// retrieve the admin token (it's printed to stdout once, but logs rotate).
//
// Bootstrap is deliberately minimal — no cryptographic key derivation, no
// rotation, no key versioning. The two secrets are random 32-byte values
// generated with crypto/rand and emitted as hex. Operators can override
// either one through environment variables; an explicit env value always
// wins over what's on disk.
package bootstrap

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bootstrap carries the two secrets the gateway needs to come up.
type Bootstrap struct {
	// ControlPlaneSecretKey is the AES-GCM key (base64 of 32 raw bytes)
	// used to encrypt persisted provider API keys at rest. base64 because
	// secrets.NewAESGCMCipher decodes its input as base64 and requires
	// exactly 32 bytes after decode.
	ControlPlaneSecretKey string `json:"control_plane_secret_key"`

	// AdminToken is the bearer token a client must present on /admin/*
	// endpoints. Hex-encoded 32 random bytes — the value is compared as an
	// opaque string so any printable form works; hex is just easy to copy.
	AdminToken string `json:"admin_token"`
}

// Resolve returns the bootstrap state to use this run, prioritizing
// explicit env-supplied values over the persisted file. When the file
// doesn't exist and the relevant env var is also empty, a random value is
// generated and persisted. The returned `printToken` flag is true exactly
// when the admin token was newly generated this run, so callers can log
// it conspicuously to stdout.
func Resolve(path, envSecret, envToken string) (b Bootstrap, printToken bool, err error) {
	envSecret = strings.TrimSpace(envSecret)
	envToken = strings.TrimSpace(envToken)

	loaded, loadErr := load(path)
	switch {
	case loadErr == nil:
		b = loaded
	case os.IsNotExist(loadErr):
		// Fresh install — fall through, we'll generate as needed.
	default:
		return Bootstrap{}, false, fmt.Errorf("read bootstrap file %q: %w", path, loadErr)
	}

	// Env always wins. The on-disk values stay as a fallback for any field
	// the env didn't override.
	if envSecret != "" {
		b.ControlPlaneSecretKey = envSecret
	}
	if envToken != "" {
		b.AdminToken = envToken
	}

	dirty := false
	if b.ControlPlaneSecretKey == "" {
		key, err := randomBase64(32)
		if err != nil {
			return Bootstrap{}, false, fmt.Errorf("generate control-plane secret key: %w", err)
		}
		b.ControlPlaneSecretKey = key
		dirty = true
	}
	if b.AdminToken == "" {
		token, err := randomHex(32)
		if err != nil {
			return Bootstrap{}, false, fmt.Errorf("generate admin token: %w", err)
		}
		b.AdminToken = token
		dirty = true
		printToken = true
	}

	// Only write back if we generated something OR if env supplied a value
	// we hadn't seen on disk. The latter keeps the file authoritative for
	// operators who want to read the current values out of band.
	if dirty || envSecret != "" || envToken != "" {
		if err := save(path, b); err != nil {
			return Bootstrap{}, false, fmt.Errorf("persist bootstrap file %q: %w", path, err)
		}
	}

	return b, printToken, nil
}

func load(path string) (Bootstrap, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bootstrap{}, err
	}
	var b Bootstrap
	if err := json.Unmarshal(data, &b); err != nil {
		return Bootstrap{}, fmt.Errorf("decode bootstrap file: %w", err)
	}
	return b, nil
}

func save(path string, b Bootstrap) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	// 0o600 because the file holds the admin token and the encryption key.
	// Anything more permissive lets a co-located service read both.
	return os.WriteFile(path, data, 0o600)
}

func randomHex(n int) (string, error) {
	buf, err := randomBytes(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomBase64(n int) (string, error) {
	buf, err := randomBytes(n)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

func randomBytes(n int) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}
