package webapp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/kizilkayaom/sefir/mcp/internal/workspace"
)

type memoryAccounts struct {
	mu                sync.Mutex
	items             map[string]Account
	err               error
	deletionErr       error
	deletionRequested map[string]bool
}

func newMemoryAccounts() *memoryAccounts {
	return &memoryAccounts{items: map[string]Account{}, deletionRequested: map[string]bool{}}
}

func (a *memoryAccounts) WithActiveAccount(_ context.Context, id string, action func() error) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return a.err
	}
	if _, exists := a.items[id]; !exists || a.deletionRequested[id] {
		return pgx.ErrNoRows
	}
	return action()
}

func (a *memoryAccounts) ByID(_ context.Context, id string) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	if account, ok := a.items[id]; ok && !a.deletionRequested[id] {
		return account, nil
	}
	return Account{}, pgx.ErrNoRows
}

func (a *memoryAccounts) ByUsername(_ context.Context, name string) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	for _, account := range a.items {
		if strings.EqualFold(account.Username, strings.TrimSpace(name)) && !a.deletionRequested[account.ID] {
			return account, nil
		}
	}
	return Account{}, pgx.ErrNoRows
}

func (a *memoryAccounts) ByGoogleSubject(_ context.Context, subject string) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	for _, account := range a.items {
		if account.GoogleSubject == subject && !a.deletionRequested[account.ID] {
			return account, nil
		}
	}
	return Account{}, pgx.ErrNoRows
}

func (a *memoryAccounts) Create(_ context.Context, credentials ownerCredentials) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.create(credentials)
}

func (a *memoryAccounts) create(credentials ownerCredentials) (Account, error) {
	if a.err != nil {
		return Account{}, a.err
	}
	for _, existing := range a.items {
		if strings.EqualFold(existing.Username, credentials.Username) ||
			(credentials.GoogleSubject != "" && existing.GoogleSubject == credentials.GoogleSubject) {
			return Account{}, ErrAccountConflict
		}
	}
	account := Account{
		ID:              fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", len(a.items)+1),
		DefaultPresetID: "prof", ownerCredentials: credentials,
	}
	a.items[account.ID] = account
	return account, nil
}

func (a *memoryAccounts) Google(_ context.Context, identity GoogleIdentity) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	for _, account := range a.items {
		if account.GoogleSubject == identity.Subject && !a.deletionRequested[account.ID] {
			return account, nil
		}
	}
	name := googleUsername(identity.Email)
	for _, account := range a.items {
		if strings.EqualFold(account.Username, name) {
			name += fmt.Sprintf("-%d", len(a.items)+1)
			break
		}
	}
	return a.create(ownerCredentials{Version: 1, Username: name, GoogleSubject: identity.Subject, GoogleEmail: identity.Email})
}

func (a *memoryAccounts) LinkGoogle(_ context.Context, id string, identity GoogleIdentity) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	account, ok := a.items[id]
	if !ok || a.deletionRequested[id] {
		return Account{}, ErrAccountConflict
	}
	if account.GoogleSubject != "" && account.GoogleSubject != identity.Subject {
		return Account{}, ErrAccountConflict
	}
	for _, other := range a.items {
		if other.ID != id && other.GoogleSubject == identity.Subject {
			return Account{}, ErrAccountConflict
		}
	}
	account.GoogleSubject, account.GoogleEmail = identity.Subject, identity.Email
	a.items[id] = account
	return account, nil
}

func (a *memoryAccounts) ImportLegacy(_ context.Context, owner *ownerCredentials) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if owner == nil {
		return nil
	}
	if a.err != nil {
		return a.err
	}
	if _, exists := a.items[workspace.LegacyAccountID]; exists {
		return nil
	}
	a.items[workspace.LegacyAccountID] = Account{
		ID: workspace.LegacyAccountID, DefaultPresetID: "prof", ownerCredentials: *owner,
	}
	return nil
}

func (a *memoryAccounts) SetDefaultPreset(ctx context.Context, accountID, presetID string) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := validateDefaultPreset(accountID, presetID); err != nil {
		return Account{}, err
	}
	if err := ctx.Err(); err != nil {
		return Account{}, err
	}
	if a.err != nil {
		return Account{}, a.err
	}
	accountID = strings.ToLower(accountID)
	account, exists := a.items[accountID]
	if !exists || a.deletionRequested[accountID] {
		return Account{}, pgx.ErrNoRows
	}
	account.DefaultPresetID = presetID
	a.items[accountID] = account
	return account, nil
}

func (a *memoryAccounts) RequestDeletion(_ context.Context, accountID string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.err != nil {
		return a.err
	}

	if a.deletionErr != nil {
		return a.deletionErr
	}

	if _, exists := a.items[accountID]; !exists {
		return pgx.ErrNoRows
	}

	a.deletionRequested[accountID] = true
	return nil
}
