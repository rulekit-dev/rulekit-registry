package mock

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/rulekit-dev/rulekit-registry/internal/domain"
	"github.com/rulekit-dev/rulekit-registry/internal/port"
)

// Datastore is an in-memory implementation of port.Datastore for testing.
type Datastore struct {
	mu            sync.Mutex
	workspaces    map[string]*domain.Workspace
	rulesets      map[string]*domain.Ruleset // "ws\x00key"
	drafts        map[string]*domain.Draft
	versions      map[string][]*domain.Version // "ws\x00key"
	users         map[string]*domain.User      // by ID
	usersByEmail  map[string]*domain.User
	otpCodes      map[string]*domain.OTPCode // by ID
	refreshTokens map[string]*domain.RefreshToken // by ID
	refreshByHash map[string]*domain.RefreshToken
	apiKeys       map[string]*domain.APIKey // by ID
	apiKeysByHash map[string]*domain.APIKey
	userRoles     map[string]*domain.UserRole // "userID\x00workspace"
}

func NewDatastore() *Datastore {
	return &Datastore{
		workspaces:    make(map[string]*domain.Workspace),
		rulesets:      make(map[string]*domain.Ruleset),
		drafts:        make(map[string]*domain.Draft),
		versions:      make(map[string][]*domain.Version),
		users:         make(map[string]*domain.User),
		usersByEmail:  make(map[string]*domain.User),
		otpCodes:      make(map[string]*domain.OTPCode),
		refreshTokens: make(map[string]*domain.RefreshToken),
		refreshByHash: make(map[string]*domain.RefreshToken),
		apiKeys:       make(map[string]*domain.APIKey),
		apiKeysByHash: make(map[string]*domain.APIKey),
		userRoles:     make(map[string]*domain.UserRole),
	}
}

func key(a, b string) string { return a + "\x00" + b }

func (d *Datastore) CreateWorkspace(_ context.Context, ws *domain.Workspace) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.workspaces[ws.Name]; ok {
		return port.ErrAlreadyExists
	}
	d.workspaces[ws.Name] = ws
	return nil
}

func (d *Datastore) GetWorkspace(_ context.Context, name string) (*domain.Workspace, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ws, ok := d.workspaces[name]
	if !ok {
		return nil, port.ErrNotFound
	}
	return ws, nil
}

func (d *Datastore) ListWorkspaces(_ context.Context, limit, offset int) ([]*domain.Workspace, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*domain.Workspace
	for _, ws := range d.workspaces {
		out = append(out, ws)
	}
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (d *Datastore) DeleteWorkspace(_ context.Context, name string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.workspaces[name]; !ok {
		return port.ErrNotFound
	}
	delete(d.workspaces, name)
	return nil
}

func (d *Datastore) ListRulesets(_ context.Context, workspace string, limit, offset int) ([]*domain.Ruleset, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*domain.Ruleset
	for _, rs := range d.rulesets {
		if rs.Workspace == workspace {
			out = append(out, rs)
		}
	}
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (d *Datastore) CreateRuleset(_ context.Context, r *domain.Ruleset) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(r.Workspace, r.Key)
	if _, ok := d.rulesets[k]; ok {
		return port.ErrAlreadyExists
	}
	d.rulesets[k] = r
	return nil
}

func (d *Datastore) GetRuleset(_ context.Context, workspace, rkey string) (*domain.Ruleset, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rs, ok := d.rulesets[key(workspace, rkey)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return rs, nil
}

func (d *Datastore) DeleteRuleset(_ context.Context, workspace, rkey string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(workspace, rkey)
	if _, ok := d.rulesets[k]; !ok {
		return port.ErrNotFound
	}
	delete(d.rulesets, k)
	delete(d.drafts, k)
	return nil
}

func (d *Datastore) GetDraft(_ context.Context, workspace, rkey string) (*domain.Draft, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	dr, ok := d.drafts[key(workspace, rkey)]
	if !ok {
		return nil, port.ErrNotFound
	}
	cp := *dr
	cp.DSL = make(json.RawMessage, len(dr.DSL))
	copy(cp.DSL, dr.DSL)
	return &cp, nil
}

func (d *Datastore) UpsertDraft(_ context.Context, draft *domain.Draft) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drafts[key(draft.Workspace, draft.RulesetKey)] = draft
	return nil
}

func (d *Datastore) DeleteDraft(_ context.Context, workspace, rkey string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(workspace, rkey)
	if _, ok := d.drafts[k]; !ok {
		return port.ErrNotFound
	}
	delete(d.drafts, k)
	return nil
}

func (d *Datastore) ListVersions(_ context.Context, workspace, rkey string, limit, offset int) ([]*domain.Version, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	vs := d.versions[key(workspace, rkey)]
	if offset >= len(vs) {
		return nil, nil
	}
	out := vs[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (d *Datastore) GetVersion(_ context.Context, workspace, rkey string, version int) (*domain.Version, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, v := range d.versions[key(workspace, rkey)] {
		if v.Version == version {
			return v, nil
		}
	}
	return nil, port.ErrNotFound
}

func (d *Datastore) GetLatestVersion(_ context.Context, workspace, rkey string) (*domain.Version, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	vs := d.versions[key(workspace, rkey)]
	if len(vs) == 0 {
		return nil, port.ErrNotFound
	}
	return vs[len(vs)-1], nil
}

func (d *Datastore) CreateVersion(_ context.Context, v *domain.Version) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(v.Workspace, v.RulesetKey)
	for _, existing := range d.versions[k] {
		if existing.Version == v.Version {
			return port.ErrVersionImmutable
		}
	}
	d.versions[k] = append(d.versions[k], v)
	return nil
}

func (d *Datastore) NextVersionNumber(_ context.Context, workspace, rkey string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	vs := d.versions[key(workspace, rkey)]
	return len(vs) + 1, nil
}

func (d *Datastore) CreateUser(_ context.Context, u *domain.User) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.usersByEmail[u.Email]; ok {
		return port.ErrAlreadyExists
	}
	d.users[u.ID] = u
	d.usersByEmail[u.Email] = u
	return nil
}

func (d *Datastore) GetUserByEmail(_ context.Context, email string) (*domain.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u, ok := d.usersByEmail[email]
	if !ok {
		return nil, port.ErrNotFound
	}
	return u, nil
}

func (d *Datastore) GetUserByID(_ context.Context, id string) (*domain.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	u, ok := d.users[id]
	if !ok {
		return nil, port.ErrNotFound
	}
	return u, nil
}

func (d *Datastore) UpdateUserLastLogin(_ context.Context, userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.users[userID]
	if !ok {
		return port.ErrNotFound
	}
	return nil
}

func (d *Datastore) ListUsers(_ context.Context, limit, offset int) ([]*domain.User, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*domain.User
	for _, u := range d.users {
		out = append(out, u)
	}
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (d *Datastore) DeleteUser(_ context.Context, userID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	u, ok := d.users[userID]
	if !ok {
		return port.ErrNotFound
	}
	delete(d.users, userID)
	delete(d.usersByEmail, u.Email)
	return nil
}

func (d *Datastore) CreateOTPCode(_ context.Context, otp *domain.OTPCode) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.otpCodes[otp.ID] = otp
	return nil
}

func (d *Datastore) GetUnusedOTPCode(_ context.Context, userID string) (*domain.OTPCode, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, otp := range d.otpCodes {
		if otp.UserID == userID && otp.UsedAt == nil {
			return otp, nil
		}
	}
	return nil, port.ErrNotFound
}

func (d *Datastore) MarkOTPUsed(_ context.Context, otpID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	otp, ok := d.otpCodes[otpID]
	if !ok {
		return port.ErrNotFound
	}
	now := otp.ExpiresAt // just mark as non-nil
	otp.UsedAt = &now
	return nil
}

func (d *Datastore) DeleteExpiredOTPs(_ context.Context) error {
	return nil
}

func (d *Datastore) CreateRefreshToken(_ context.Context, t *domain.RefreshToken) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.refreshTokens[t.ID] = t
	d.refreshByHash[t.TokenHash] = t
	return nil
}

func (d *Datastore) GetRefreshTokenByHash(_ context.Context, tokenHash string) (*domain.RefreshToken, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	rt, ok := d.refreshByHash[tokenHash]
	if !ok {
		return nil, port.ErrNotFound
	}
	return rt, nil
}

func (d *Datastore) RevokeRefreshToken(_ context.Context, tokenID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	rt, ok := d.refreshTokens[tokenID]
	if !ok {
		return port.ErrNotFound
	}
	now := rt.ExpiresAt
	rt.RevokedAt = &now
	return nil
}

func (d *Datastore) CreateAPIKey(_ context.Context, k *domain.APIKey) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.apiKeys[k.ID] = k
	d.apiKeysByHash[k.KeyHash] = k
	return nil
}

func (d *Datastore) GetAPIKeyByHash(_ context.Context, hash string) (*domain.APIKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	k, ok := d.apiKeysByHash[hash]
	if !ok {
		return nil, port.ErrNotFound
	}
	return k, nil
}

func (d *Datastore) ListAPIKeys(_ context.Context, limit, offset int) ([]*domain.APIKey, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*domain.APIKey
	for _, k := range d.apiKeys {
		out = append(out, k)
	}
	if offset >= len(out) {
		return nil, nil
	}
	out = out[offset:]
	if limit < len(out) {
		out = out[:limit]
	}
	return out, nil
}

func (d *Datastore) RevokeAPIKey(_ context.Context, keyID string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k, ok := d.apiKeys[keyID]
	if !ok {
		return port.ErrNotFound
	}
	now := k.CreatedAt
	k.RevokedAt = &now
	return nil
}

func (d *Datastore) UpsertUserRole(_ context.Context, ur *domain.UserRole) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.userRoles[key(ur.UserID, ur.Workspace)] = ur
	return nil
}

func (d *Datastore) GetUserRole(_ context.Context, userID, workspace string) (*domain.UserRole, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	ur, ok := d.userRoles[key(userID, workspace)]
	if !ok {
		return nil, port.ErrNotFound
	}
	return ur, nil
}

func (d *Datastore) ListUserRoles(_ context.Context, userID string) ([]*domain.UserRole, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []*domain.UserRole
	for k, ur := range d.userRoles {
		if len(k) > len(userID) && k[:len(userID)] == userID && k[len(userID)] == 0 {
			out = append(out, ur)
		}
	}
	return out, nil
}

func (d *Datastore) DeleteUserRole(_ context.Context, userID, workspace string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	k := key(userID, workspace)
	if _, ok := d.userRoles[k]; !ok {
		return port.ErrNotFound
	}
	delete(d.userRoles, k)
	return nil
}

func (d *Datastore) Ping(_ context.Context) error { return nil }
func (d *Datastore) Close() error                 { return nil }
