package linodeinventory

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type Preview struct {
	Token            string    `json:"token"`
	Resource         Resource  `json:"resource"`
	Effects          []string  `json:"effects"`
	ExpiresAt        time.Time `json:"expiresAt"`
	NameConfirmation string    `json:"nameConfirmation"`
	IDConfirmation   string    `json:"idConfirmation"`
}
type plan struct {
	Preview
	scope       [32]byte
	fingerprint [32]byte
}
type Service struct {
	Client Client
	mu     sync.Mutex
	plans  map[string]plan
}

func fingerprint(r Resource) [32]byte { raw, _ := json.Marshal(r); return sha256.Sum256(raw) }
func (s *Service) Preview(ctx context.Context, token string, ref Ref) (Preview, error) {
	item, effects, err := s.Client.inspect(ctx, token, ref)
	if err != nil {
		return Preview{}, err
	}
	var bytes [24]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return Preview{}, err
	}
	preview := Preview{Token: hex.EncodeToString(bytes[:]), Resource: item, Effects: effects, ExpiresAt: time.Now().Add(5 * time.Minute), NameConfirmation: "confirm", IDConfirmation: "confirm"}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.plans == nil {
		s.plans = map[string]plan{}
	}
	for id, p := range s.plans {
		if time.Now().After(p.ExpiresAt) {
			delete(s.plans, id)
		}
	}
	if len(s.plans) >= 100 {
		return Preview{}, fmt.Errorf("too many open deletion reviews; wait for older reviews to expire")
	}
	s.plans[preview.Token] = plan{preview, sha256.Sum256([]byte(token)), fingerprint(item)}
	return preview, nil
}
func (s *Service) Delete(ctx context.Context, token, review, nameConfirmation, idConfirmation string) (Resource, error) {
	s.mu.Lock()
	p, ok := s.plans[review]
	if !ok || time.Now().After(p.ExpiresAt) {
		delete(s.plans, review)
		s.mu.Unlock()
		return Resource{}, fmt.Errorf("review expired or already used; review this resource again")
	}
	if p.scope != sha256.Sum256([]byte(token)) || nameConfirmation != p.NameConfirmation || idConfirmation != p.IDConfirmation {
		s.mu.Unlock()
		return Resource{}, fmt.Errorf("both typed confirmations must match this review and the same Linode token must still be selected")
	}
	// Consume before I/O; concurrent requests and retries can never send two deletes.
	delete(s.plans, review)
	s.mu.Unlock()
	item, _, err := s.Client.inspect(ctx, token, p.Resource.Ref)
	if err != nil {
		return Resource{}, err
	}
	if fingerprint(item) != p.fingerprint {
		return Resource{}, fmt.Errorf("resource changed since review; refresh and review it again")
	}
	path, _ := resourcePath(item.Ref)
	if err = s.Client.request(ctx, token, http.MethodDelete, path, nil); err != nil {
		return Resource{}, fmt.Errorf("deletion was not confirmed; refresh inventory before retrying: %w", err)
	}
	return item, nil
}
