package quote

import (
	"bytes"
	"crypto/sha256"
	"errors"
)

type entry struct {
	message []byte
	pp      PackageProperties
	ip      InfrastructureProperties
}

// MockValidator is a mockup quote validator
type MockValidator struct {
	valid map[string]entry
}

func NewMockValidator() *MockValidator {
	return &MockValidator{
		make(map[string]entry),
	}
}

// Validate implements the Validator interface
func (m *MockValidator) Validate(quote []byte, message []byte, pp PackageProperties, ip InfrastructureProperties) error {
	entry, found := m.valid[string(quote)]
	if !found {
		return errors.New("wrong quote")
	}
	if !bytes.Equal(entry.message, message) {
		return errors.New("wrong message")
	}
	if !pp.IsCompliant(entry.pp) {
		return errors.New("package does not comply")
	}
	if !ip.IsCompliant(entry.ip) {
		return errors.New("infrastructure does not comply")
	}
	return nil
}

// AddValidQuote adds a valid quote
func (m *MockValidator) AddValidQuote(quote []byte, message []byte, pp PackageProperties, ip InfrastructureProperties) {
	m.valid[string(quote)] = entry{message, pp, ip}
}

// MockIssuer is a mockup quote issuer
type MockIssuer struct{}

func NewMockIssuer() *MockIssuer {
	return &MockIssuer{}
}

// Issue implements the Issuer interface
func (m *MockIssuer) Issue(message []byte) ([]byte, error) {
	quote := sha256.Sum256(message)
	return quote[:], nil
}