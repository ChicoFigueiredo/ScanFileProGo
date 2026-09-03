//go:build windows

package config

import (
	"encoding/base64"
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

// secretEntropy binds the protected blob to this application: a blob copied
// into another program's configuration cannot be decrypted by it.
var secretEntropy = []byte("ScanFile Pro/openrouter-key/v1")

// protectSecret encrypts plain with DPAPI, scoped to the current Windows user,
// and returns it base64 encoded. If DPAPI is unavailable the value is stored
// base64 only, and the second result reports that so the reader knows how to
// decode it and the user can be warned.
func protectSecret(plain string) (blob string, isPlain bool) {
	enc, err := dpapi(windows.CryptProtectData, []byte(plain))
	if err != nil {
		return base64.StdEncoding.EncodeToString([]byte(plain)), true
	}
	return base64.StdEncoding.EncodeToString(enc), false
}

// unprotectSecret reverses protectSecret.
func unprotectSecret(blob string, isPlain bool) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(blob)
	if err != nil {
		return "", err
	}
	if isPlain {
		return string(raw), nil
	}
	dec, err := dpapi(func(in *windows.DataBlob, name *uint16, entropy *windows.DataBlob, reserved uintptr, prompt *windows.CryptProtectPromptStruct, flags uint32, out *windows.DataBlob) error {
		var describe *uint16
		return windows.CryptUnprotectData(in, &describe, entropy, reserved, prompt, flags, out)
	}, raw)
	if err != nil {
		return "", err
	}
	return string(dec), nil
}

type dpapiFunc func(in *windows.DataBlob, name *uint16, entropy *windows.DataBlob, reserved uintptr, prompt *windows.CryptProtectPromptStruct, flags uint32, out *windows.DataBlob) error

// dpapi runs one of the CryptProtectData / CryptUnprotectData pair over data and
// copies the result out of the LocalAlloc buffer the API returns.
func dpapi(fn dpapiFunc, data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("nada a proteger")
	}

	in := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	entropy := windows.DataBlob{Size: uint32(len(secretEntropy)), Data: &secretEntropy[0]}
	var out windows.DataBlob

	if err := fn(&in, nil, &entropy, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))

	result := make([]byte, out.Size)
	copy(result, unsafe.Slice(out.Data, out.Size))
	return result, nil
}
