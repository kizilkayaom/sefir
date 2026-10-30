package webapp

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kizilkayaom/sefir/mcp/internal/draftcontent"
)

const passwordIterations = 600000

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{3,32}$`)

type ownerCredentials struct {
	Version       int    `json:"version"`
	Username      string `json:"username"`
	Salt          []byte `json:"salt"`
	Hash          []byte `json:"hash"`
	GoogleSubject string `json:"google_subject,omitempty"`
	GoogleEmail   string `json:"google_email,omitempty"`
}

func validateUsername(username string) error {
	if !usernamePattern.MatchString(username) {
		return errors.New("Username must be 3–32 characters using letters, numbers, dots, underscores, or hyphens")
	}
	return nil
}

func validatePassword(password string) error {
	length := utf8.RuneCountInString(password)
	if !utf8.ValidString(password) || length < 8 || length > 128 {
		return errors.New("Password must be 8–128 characters")
	}
	var letter, number, symbol bool
	for _, character := range password {
		letter = letter || unicode.IsLetter(character)
		number = number || unicode.IsDigit(character)
		symbol = symbol || unicode.IsPunct(character) || unicode.IsSymbol(character)
	}
	if !letter || !number || !symbol {
		return errors.New("Password must include a letter, a number, and a symbol")
	}
	return nil
}

func newOwner(username, password string) (*ownerCredentials, error) {
	if err := validateUsername(username); err != nil {
		return nil, err
	}
	if err := validatePassword(password); err != nil {
		return nil, err
	}
	salt := make([]byte, 32)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	hash, err := pbkdf2.Key(sha256.New, password, salt, passwordIterations, 32)
	if err != nil {
		return nil, err
	}
	return &ownerCredentials{Version: 1, Username: username, Salt: salt, Hash: hash}, nil
}

func (owner *ownerCredentials) matches(username, password string) bool {
	if len(owner.Salt) != 32 || len(owner.Hash) != 32 || !utf8.ValidString(password) || utf8.RuneCountInString(password) > 128 {
		return false
	}
	hash, err := pbkdf2.Key(sha256.New, password, owner.Salt, passwordIterations, 32)
	if err != nil {
		return false
	}
	actualName := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(username))))
	expectedName := sha256.Sum256([]byte(strings.ToLower(owner.Username)))
	return subtle.ConstantTimeCompare(hash, owner.Hash)&subtle.ConstantTimeCompare(actualName[:], expectedName[:]) == 1
}

func loadOwner(config Config) (*ownerCredentials, error) {
	username := strings.TrimSpace(config.Owner)
	if username != "" || config.Password != "" {
		if username == "" || config.Password == "" {
			return nil, errors.New("set both SEFIR_OWNER and SEFIR_OWNER_PASSWORD, or remove both to create your account in the browser")
		}
		return newOwner(username, config.Password)
	}
	if config.CredentialsPath == "" {
		return nil, errors.New("a legacy owner credentials path is required")
	}
	file, err := os.Open(config.CredentialsPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.New("could not read the owner account file")
	}
	defer file.Close()
	var owner ownerCredentials
	decoder := json.NewDecoder(io.LimitReader(file, 8192))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&owner); err != nil || decoder.Decode(new(any)) != io.EOF ||
		!owner.valid() {
		return nil, errors.New("the owner account file is invalid")
	}
	return &owner, nil
}

func saveOwner(path string, owner *ownerCredentials) error {
	return writeOwner(path, owner, false)
}

func (owner *ownerCredentials) valid() bool {
	password := len(owner.Salt) == 32 && len(owner.Hash) == 32
	noPassword := len(owner.Salt) == 0 && len(owner.Hash) == 0
	google := owner.GoogleSubject != "" && len(owner.GoogleSubject) <= 255
	if google {
		address, err := draftcontent.NormalizeRecipient(owner.GoogleEmail)
		google = err == nil && address == owner.GoogleEmail
	}
	noGoogle := owner.GoogleSubject == "" && owner.GoogleEmail == ""
	return owner.Version == 1 && validateUsername(owner.Username) == nil &&
		(password || noPassword) && (google || noGoogle) && (password || google)
}

func writeOwner(path string, owner *ownerCredentials, replace bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".owner-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	defer file.Close()
	if err := json.NewEncoder(file).Encode(owner); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if replace {
		return os.Rename(file.Name(), path)
	}
	return os.Link(file.Name(), path)
}
