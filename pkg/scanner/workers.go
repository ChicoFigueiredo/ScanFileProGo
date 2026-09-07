package scanner

import "runtime"

// Fases da Varredura usadas por ResolveWorkers.
const (
	PhaseMetadata = 1 // Fase 1: percurso de pastas e metadados
	PhaseHashing  = 2 // Fase 2: leitura de conteúdo e hashing
)

// MaxThreads é o teto absoluto de threads que o motor aceita: 4 x NumCPU.
func MaxThreads() int {
	return runtime.NumCPU() * 4
}

// ThreadOptions devolve as opções oferecidas à interface: 0 (Auto) seguido das
// potências de 2 de 4 até MaxThreads(), inclusive.
func ThreadOptions() []int {
	max := MaxThreads()
	opts := []int{0}
	for v := 4; v <= max; v *= 2 {
		opts = append(opts, v)
	}
	return opts
}

// ResolveWorkers converte o número pedido pelo usuário no número efetivo de
// threads da fase. Pedido <= 0 significa Auto: 2 x NumCPU na Fase 1 e NumCPU na
// Fase 2. Qualquer pedido é limitado por MaxThreads().
func ResolveWorkers(requested, phase int) int {
	if requested <= 0 {
		n := runtime.NumCPU()
		if phase == PhaseMetadata {
			return n * 2
		}
		return n
	}
	if max := MaxThreads(); requested > max {
		return max
	}
	return requested
}
