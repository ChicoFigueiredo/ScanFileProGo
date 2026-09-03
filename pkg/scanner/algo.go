package scanner

import "strings"

// Identificadores dos algoritmos de Hash Completo suportados.
const (
	HashXXHash = "xxhash"
	HashBlake3 = "blake3"
	HashMD5    = "md5"
	HashSHA256 = "sha256"
)

// DefaultHashAlgorithm é o algoritmo aplicado quando a Configuração não define um.
const DefaultHashAlgorithm = HashXXHash

// hashPrefixes mapeia algoritmo -> prefixo gravado na string de hash.
var hashPrefixes = map[string]string{
	HashXXHash: "xxh64:",
	HashBlake3: "blake3:",
	HashMD5:    "md5:",
	HashSHA256: "sha256:",
}

// SupportedHashAlgorithms lista os algoritmos na ordem de apresentação da interface.
func SupportedHashAlgorithms() []string {
	return []string{HashXXHash, HashBlake3, HashMD5, HashSHA256}
}

// NormalizeHashAlgorithm converte apelidos e variações de caixa no identificador
// canônico. Valores desconhecidos ou vazios caem no algoritmo padrão.
func NormalizeHashAlgorithm(algo string) string {
	switch strings.ToLower(strings.TrimSpace(algo)) {
	case "blake3", "b3":
		return HashBlake3
	case "md5":
		return HashMD5
	case "sha256", "sha-256", "sha_256":
		return HashSHA256
	case "xxhash", "xxh64", "xxhash64", "xx":
		return HashXXHash
	default:
		return DefaultHashAlgorithm
	}
}

// HashPrefix devolve o prefixo textual do algoritmo (ex.: "xxh64:").
func HashPrefix(algo string) string {
	return hashPrefixes[NormalizeHashAlgorithm(algo)]
}

// HashAlgorithmOf identifica o algoritmo a partir do prefixo de uma string de
// hash já gravada. Devolve "" quando não há prefixo reconhecível.
func HashAlgorithmOf(hash string) string {
	lower := strings.ToLower(hash)
	for algo, prefix := range hashPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return algo
		}
	}
	return ""
}

// HashMatchesAlgorithm informa se um hash já gravado foi calculado com o
// algoritmo pedido. É a guarda do Quick Scan contra reaproveitar hashes de
// outro algoritmo (achado M14).
func HashMatchesAlgorithm(hash, algo string) bool {
	if hash == "" {
		return false
	}
	return HashAlgorithmOf(hash) == NormalizeHashAlgorithm(algo)
}
