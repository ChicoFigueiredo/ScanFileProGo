package mcp

import (
	"fmt"

	"scanfile/pkg/ai"
	"scanfile/pkg/indexer"
	"scanfile/pkg/scanner"
)

// NewMCPToolsContextFromAutosave builds a tools context out of the latest Autosave
// found in dir (autosave_latest.sfz, falling back to autosave_previous.sfz),
// rebuilding the duplicate and folder indexes and adopting the Snapshot's Raízes
// Varridas as the allowed roots.
//
// It is what `--mcp` uses so that an MCP client sees the last Varredura without
// re-scanning the disk.
func NewMCPToolsContextFromAutosave(dir string, ollama *ai.OllamaClient, model string) (*MCPToolsContext, *scanner.CacheSnapshot, error) {
	info, err := scanner.GetLatestAutoSave(dir)
	if err != nil || info == nil {
		return nil, nil, fmt.Errorf("nenhum Autosave encontrado em %q: execute uma Varredura antes de iniciar o servidor MCP", dir)
	}

	tree, snapshot, err := scanner.LoadCacheFromFile(info.FilePath)
	if err != nil {
		return nil, nil, fmt.Errorf("falha ao carregar o Autosave %s: %w", info.FilePath, err)
	}

	idx := indexer.NewDuplicateIndex()
	idx.RebuildIndex(tree.GetAllFiles())

	folderIdx := indexer.NewFolderDuplicateIndex()
	folderIdx.RebuildFolderIndex(tree)

	tc := NewMCPToolsContext(tree, idx, folderIdx, ollama, model)
	tc.SetAllowedRoots(snapshot.Roots)

	return tc, snapshot, nil
}
