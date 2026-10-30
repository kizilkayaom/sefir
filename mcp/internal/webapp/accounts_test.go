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
	mu    sync.Mutex
	items map[string]Account
	err   error
}

func newMemoryAccounts() *memoryAccounts { return &memoryAccounts{items: map[string]Account{}} }

func (a *memoryAccounts) ByID(_ context.Context, id string) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.err != nil {
		return Account{}, a.err
	}
	if account, ok := a.items[id]; ok {
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
		if strings.EqualFold(account.Username, strings.TrimSpace(name)) {
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
		if account.GoogleSubject == subject {
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
	account := Account{ID: fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", len(a.items)+1), ownerCredentials: credentials}
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
		if account.GoogleSubject == identity.Subject {
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
	if !ok {
		return Account{}, pgx.ErrNoRows
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
	a.items[workspace.LegacyAccountID] = Account{ID: workspace.LegacyAccountID, ownerCredentials: *owner}
	return nil
}
