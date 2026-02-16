package proxy

import (
	"time"

	"godex/pkg/admin"
)

type adminAdapter struct {
	keys *KeyStore
}

func (a adminAdapter) Add(label, rate string, burst int, quota int64, providedKey string, ttl time.Duration) (admin.KeyInfo, string, error) {
	rec, secret, err := a.keys.Add(label, rate, burst, quota, providedKey, ttl)
	if err != nil {
		return admin.KeyInfo{}, "", err
	}
	return admin.KeyInfo{ID: rec.ID, TokenBalance: rec.TokenBalance, TokenAllowance: rec.TokenAllowance, AllowanceDurationSec: rec.AllowanceDurationSec}, secret, nil
}

func (a adminAdapter) SetTokenPolicy(id string, balance int64, allowance int64, duration time.Duration) (admin.KeyInfo, error) {
	rec, err := a.keys.SetTokenPolicy(id, balance, allowance, duration)
	if err != nil {
		return admin.KeyInfo{}, err
	}
	return admin.KeyInfo{ID: rec.ID, TokenBalance: rec.TokenBalance, TokenAllowance: rec.TokenAllowance, AllowanceDurationSec: rec.AllowanceDurationSec}, nil
}

func (a adminAdapter) AddTokens(id string, delta int64) (admin.KeyInfo, error) {
	rec, err := a.keys.AddTokens(id, delta)
	if err != nil {
		return admin.KeyInfo{}, err
	}
	return admin.KeyInfo{ID: rec.ID, TokenBalance: rec.TokenBalance, TokenAllowance: rec.TokenAllowance, AllowanceDurationSec: rec.AllowanceDurationSec}, nil
}
