package hasher

import (
	"io"
	"os"

	"github.com/cespare/xxhash/v2"
)

const (
	// PrehashChunkSize é o tamanho de cada ponta lida pelo Pré-hash.
	PrehashChunkSize = 4096

	// PrehashMinSize é o tamanho máximo em que o Pré-hash não compensa:
	// arquivos com até 8192 bytes já cabem inteiros nas duas pontas, então vão
	// direto ao Hash Completo.
	PrehashMinSize = 2 * PrehashChunkSize
)

// ComputeQuickHash calcula o Pré-hash: xxHash64 dos primeiros 4096 bytes
// seguidos dos últimos 4096 bytes do arquivo. Devolve também zero erros para
// arquivos menores que 8192 bytes, caso em que lê o conteúdo inteiro uma vez.
//
// O resultado nunca é usado como prova de igualdade: serve só para descartar
// colisões de tamanho sem ler o arquivo completo.
func ComputeQuickHash(path string, size int64) (uint64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return quickHashFile(f, size)
}

func quickHashFile(f *os.File, size int64) (uint64, error) {
	digest := xxhash.New()
	buf := make([]byte, PrehashChunkSize)

	head := int64(PrehashChunkSize)
	if size < head {
		head = size
	}
	if head > 0 {
		n, err := io.ReadFull(f, buf[:head])
		if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
			return 0, err
		}
		_, _ = digest.Write(buf[:n])
	}

	// Cauda: só vale a pena quando não se sobrepõe totalmente ao cabeçalho.
	if size > PrehashChunkSize {
		offset := size - PrehashChunkSize
		if offset < PrehashChunkSize {
			offset = PrehashChunkSize
		}
		tail := size - offset
		if tail > 0 {
			if _, err := f.Seek(offset, io.SeekStart); err != nil {
				return 0, err
			}
			n, err := io.ReadFull(f, buf[:tail])
			if err != nil && err != io.ErrUnexpectedEOF && err != io.EOF {
				return 0, err
			}
			_, _ = digest.Write(buf[:n])
		}
	}

	sum := digest.Sum64()
	if sum == 0 {
		// Zero é o valor sentinela de "sem Pré-hash" em FileNode.QuickHash.
		sum = 1
	}
	return sum, nil
}

// prehashBytesRead informa quantos bytes o Pré-hash leu de um arquivo desse
// tamanho, para a contabilidade de bytes lidos.
func prehashBytesRead(size int64) int64 {
	if size <= PrehashChunkSize {
		return size
	}
	if size <= PrehashMinSize {
		return size
	}
	return 2 * PrehashChunkSize
}
