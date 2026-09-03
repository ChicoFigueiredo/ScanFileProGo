package hasher

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"sync"

	"github.com/cespare/xxhash/v2"
	"lukechampine.com/blake3"
	"scanfile/pkg/scanner"
)

// bufferSize é o tamanho do buffer de leitura sequencial usado no Hash Completo.
const bufferSize = 1 << 20 // 1 MiB

// bufferPool recicla os buffers de 1 MiB entre workers e chamadas avulsas,
// evitando alocar um buffer por arquivo (achado M4/H8).
var bufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, bufferSize)
		return &buf
	},
}

func getBuffer() *[]byte  { return bufferPool.Get().(*[]byte) }
func putBuffer(b *[]byte) { bufferPool.Put(b) }

// NewDigest cria o acumulador de hash do algoritmo pedido. Algoritmos
// desconhecidos caem no padrão (xxHash64).
func NewDigest(algo string) hash.Hash {
	switch scanner.NormalizeHashAlgorithm(algo) {
	case scanner.HashBlake3:
		return blake3.New(32, nil)
	case scanner.HashMD5:
		return md5.New()
	case scanner.HashSHA256:
		return sha256.New()
	default:
		return xxhash.New()
	}
}

// FormatDigest monta a string final "<prefixo>:<hex>" a partir do acumulador.
func FormatDigest(algo string, h hash.Hash) string {
	return scanner.HashPrefix(algo) + hex.EncodeToString(h.Sum(nil))
}

// HashBytes calcula o hash de um bloco em memória. Usado por testes e por
// consumidores que já têm o conteúdo carregado.
func HashBytes(algo string, data []byte) string {
	h := NewDigest(algo)
	_, _ = h.Write(data)
	return FormatDigest(algo, h)
}
