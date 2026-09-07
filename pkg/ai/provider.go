package ai

import "strings"

// ProviderType identifies where the Assistente gets its inference from.
type ProviderType string

const (
	// ProviderOllama runs a local model through the Ollama daemon.
	ProviderOllama ProviderType = "ollama"
	// ProviderOpenRouter runs a cloud model through OpenRouter.
	ProviderOpenRouter ProviderType = "openrouter"
	// ProviderQuick is the Comandos Rápidos keyword router: it triggers the
	// Assistente tools without any language model.
	ProviderQuick ProviderType = "quick"

	// ProviderDirectLocal is the legacy identifier of ProviderQuick, kept so that
	// configurations written by older versions keep working.
	//
	// Deprecated: use ProviderQuick.
	ProviderDirectLocal ProviderType = "direct"
)

// NormalizeProvider maps aliases and unknown values onto a supported Provedor.
// The legacy "direct" becomes "quick"; anything unrecognised falls back to Ollama.
func NormalizeProvider(p ProviderType) ProviderType {
	switch ProviderType(strings.ToLower(strings.TrimSpace(string(p)))) {
	case ProviderOpenRouter:
		return ProviderOpenRouter
	case ProviderQuick, ProviderDirectLocal:
		return ProviderQuick
	default:
		return ProviderOllama
	}
}

// ProviderDisplayName is the label shown in the interface for a Provedor.
func ProviderDisplayName(p ProviderType) string {
	switch NormalizeProvider(p) {
	case ProviderOpenRouter:
		return "OpenRouter (nuvem)"
	case ProviderQuick:
		return "Comandos Rápidos (sem modelo)"
	default:
		return "Ollama (local)"
	}
}

// SupportedProviders lists the Provedores accepted by the configuration, in the
// order the interface should present them.
func SupportedProviders() []ProviderType {
	return []ProviderType{ProviderOllama, ProviderOpenRouter, ProviderQuick}
}
