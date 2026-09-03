//go:build !windows

package config

import (
	"encoding/base64"
	"errors"
)

// protectSecret has no DPAPI to rely on outside Windows, so it only encodes the
// value and says so: AIOpenRouterKeyPlain true tells the reader, and the user,
// that the key is merely encoded, not protected.
func protectSecret(plain string) (blob string, isPlain bool) {
	return base64.StdEncoding.EncodeToString([]byte(plain)), true
}

// unprotectSecret reverses protectSecret. A blob protected by DPAPI on Windows
// cannot be read here.
func unprotectSecret(blob string, isPlain bool) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	if !isPlain {
		return "", errNotProtectedHere
	}
	return string(raw), nil
}

// errNotProtectedHere is returned for a blob written by the Windows build.
var errNotProtectedHere = errors.New("a chave foi protegida pelo DPAPI do Windows e não pode ser lida nesta plataforma")
