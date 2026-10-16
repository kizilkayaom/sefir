package catalogue

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Contact struct {
	ID    string  `json:"id"`
	Name  *string `json:"name"`
	Email *string `json:"email"`
}

// Fetch a contact by ID only if it has been confirmed
func GetConfirmedContact(ctx context.Context, pool *pgxpool.Pool, id string) (Contact, error) {
	var contact Contact

	const query = `
    SELECT id::text, name, email
    FROM contacts
    WHERE id = $1 AND status = 'confirmed'
	`

	row := pool.QueryRow(ctx, query, id)
	err := row.Scan(&contact.ID, &contact.Name, &contact.Email)
	if err != nil {
		return Contact{}, err
	} else {
		return contact, nil
	}
}
