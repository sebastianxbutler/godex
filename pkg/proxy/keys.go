package proxy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

type KeyRecord struct {
	ID          string     `json:"id"`
	Label       string     `json:"label"`
	Hash        string     `json:"hash"`
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Rate        string     `json:"rate,omitempty"`
	Burst       int        `json:"burst,omitempty"`
	QuotaTokens int64      `json:"quota_tokens,omitempty"`
}

type KeyFile struct {
	Version int         `json:"version"`
	Keys    []KeyRecord `json:"keys"`
}

type KeyStore struct {
	path string
	mu   sync.Mutex
	file KeyFile
}

func LoadKeyStore(path string) (*KeyStore, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("keys path required")
	}
	ks := &KeyStore{path: path, file: KeyFile{Version: 1, Keys: []KeyRecord{}}}
	buf, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ks, nil
		}
		return nil, err
	}
	if len(buf) == 0 {
		return ks, nil
	}
	if err := json.Unmarshal(buf, &ks.file); err != nil {
		return nil, err
	}
	if ks.file.Version == 0 {
		ks.file.Version = 1
	}
	ks.PruneExpired()
	return ks, nil
}

func (s *KeyStore) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}

func (s *KeyStore) saveLocked() error {
	buf, err := json.MarshalIndent(s.file, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, buf, 0o600)
}

func (s *KeyStore) List() []KeyRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]KeyRecord, len(s.file.Keys))
	copy(out, s.file.Keys)
	return out
}

func (s *KeyStore) Add(label string, rate string, burst int, quota int64, providedKey string, ttl time.Duration) (KeyRecord, string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return KeyRecord{}, "", errors.New("label is required")
	}
	id, err := newKeyID()
	if err != nil {
		return KeyRecord{}, "", err
	}
	secret := strings.TrimSpace(providedKey)
	if secret == "" {
		secret, err = newAPIKey()
		if err != nil {
			return KeyRecord{}, "", err
		}
	}
	rec := KeyRecord{
		ID:          id,
		Label:       label,
		Hash:        hashToken(secret),
		CreatedAt:   time.Now().UTC(),
		Rate:        rate,
		Burst:       burst,
		QuotaTokens: quota,
	}
	if ttl > 0 {
		expires := time.Now().UTC().Add(ttl)
		rec.ExpiresAt = &expires
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.file.Keys = append(s.file.Keys, rec)
	if err := s.saveLocked(); err != nil {
		return KeyRecord{}, "", err
	}
	return rec, secret, nil
}

func (s *KeyStore) Revoke(idOrToken string) (KeyRecord, bool) {
	idOrToken = strings.TrimSpace(idOrToken)
	if idOrToken == "" {
		return KeyRecord{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, rec := range s.file.Keys {
		if rec.ID == idOrToken || rec.Hash == hashToken(idOrToken) {
			now := time.Now().UTC()
			rec.RevokedAt = &now
			s.file.Keys[i] = rec
			_ = s.saveLocked()
			return rec, true
		}
	}
	return KeyRecord{}, false
}

func (s *KeyStore) Update(id string, label string, rate string, burst int, quota int64, ttl time.Duration) (KeyRecord, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return KeyRecord{}, errors.New("id required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, rec := range s.file.Keys {
		if rec.ID != id {
			continue
		}
		if strings.TrimSpace(label) != "" {
			rec.Label = strings.TrimSpace(label)
		}
		if strings.TrimSpace(rate) != "" {
			rec.Rate = strings.TrimSpace(rate)
		}
		if burst != 0 {
			rec.Burst = burst
		}
		if quota != 0 {
			rec.QuotaTokens = quota
		}
		if ttl > 0 {
			expires := time.Now().UTC().Add(ttl)
			rec.ExpiresAt = &expires
		}
		s.file.Keys[i] = rec
		if err := s.saveLocked(); err != nil {
			return KeyRecord{}, err
		}
		return rec, nil
	}
	return KeyRecord{}, errors.New("key not found")
}

func (s *KeyStore) Rotate(idOrToken string) (KeyRecord, string, error) {
	rec, ok := s.Revoke(idOrToken)
	if !ok {
		return KeyRecord{}, "", errors.New("key not found")
	}
	return s.Add(rec.Label, rec.Rate, rec.Burst, rec.QuotaTokens, "", 0)
}

func (s *KeyStore) Validate(token string) (KeyRecord, bool) {
	hash := hashToken(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	for _, rec := range s.file.Keys {
		if rec.Hash == hash {
			if rec.RevokedAt != nil {
				return KeyRecord{}, false
			}
			if rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
				return KeyRecord{}, false
			}
			return rec, true
		}
	}
	return KeyRecord{}, false
}

func (s *KeyStore) PruneExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	filtered := s.file.Keys[:0]
	for _, rec := range s.file.Keys {
		if rec.ExpiresAt != nil && rec.ExpiresAt.Before(now) {
			continue
		}
		filtered = append(filtered, rec)
	}
	s.file.Keys = filtered
	_ = s.saveLocked()
}

func hashToken(token string) string {
	if strings.HasPrefix(token, "sha256:") {
		return token
	}
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func newAPIKey() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "gxk_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func newKeyID() (string, error) {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("key_%s", hex.EncodeToString(buf)), nil
}
