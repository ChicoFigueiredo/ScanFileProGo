import test from 'node:test';
import assert from 'node:assert/strict';
import core from '../js/core.js';

const {
  buildThreadOptions,
  threadOptionLabel,
  phaseState,
  driveIsWSL,
  driveDefaultSelected,
  normalizeProvider,
  providerLabel,
  normalizeModelCatalog,
  pageBounds,
} = core;

// ---------------------------------------------------------------------
// Threads (1.3)
// ---------------------------------------------------------------------

test('buildThreadOptions gera 0 e potencias de 2 de 4 ate 4 x numCPU', () => {
  assert.deepEqual(buildThreadOptions(16), [0, 4, 8, 16, 32, 64]);
  assert.deepEqual(buildThreadOptions(8), [0, 4, 8, 16, 32]);
  assert.deepEqual(buildThreadOptions(4), [0, 4, 8, 16]);
});

test('buildThreadOptions nunca devolve lista sem opcao util', () => {
  assert.deepEqual(buildThreadOptions(0), [0, 4]);
  assert.deepEqual(buildThreadOptions(undefined), [0, 4]);
  assert.deepEqual(buildThreadOptions(-3), [0, 4]);
});

test('o rotulo do Auto e calculado a partir de numCPU', () => {
  assert.equal(threadOptionLabel(0, 16), 'Auto (32 na Fase 1, 16 na Fase 2)');
  assert.equal(threadOptionLabel(0, 8), 'Auto (16 na Fase 1, 8 na Fase 2)');
  assert.equal(threadOptionLabel(0, undefined), 'Auto (definido pelo servidor)');
});

test('as demais opcoes de threads levam rotulo simples', () => {
  assert.equal(threadOptionLabel(4, 16), '4 threads');
  assert.equal(threadOptionLabel(64, 16), '64 threads');
});

// ---------------------------------------------------------------------
// Fases (1.2)
// ---------------------------------------------------------------------

test('fases de trabalho bloqueiam iniciar e liberam cancelar', () => {
  ['phase1_metadata', 'phase2_hashing', 'indexing', 'cancelling'].forEach((f) => {
    const s = phaseState(f);
    assert.equal(s.busy, true, f);
    assert.equal(s.canStart, false, f);
    assert.equal(s.canCancel, true, f);
  });
});

test('cancelling ainda mostra o botao Cancelar; cancelled esconde', () => {
  assert.equal(phaseState('cancelling').canCancel, true);
  assert.equal(phaseState('cancelled').canCancel, false);
  assert.equal(phaseState('cancelled').canStart, true);
  assert.equal(phaseState('cancelled').label, 'Varredura cancelada');
});

test('indexing e uma fase conhecida e ocupada', () => {
  const s = phaseState('indexing');
  assert.equal(s.label, 'Indexando duplicados');
  assert.equal(s.badge, 'scanning');
});

test('fases terminais liberam nova varredura', () => {
  ['idle', 'completed', 'watching'].forEach((f) => {
    const s = phaseState(f);
    assert.equal(s.canStart, true, f);
    assert.equal(s.canCancel, false, f);
  });
});

test('loading_cache ocupa a interface mas nao oferece cancelamento', () => {
  const s = phaseState('loading_cache');
  assert.equal(s.busy, true);
  assert.equal(s.canCancel, false);
});

test('fase desconhecida cai num padrao seguro', () => {
  const s = phaseState('fase_do_futuro');
  assert.equal(s.canStart, true);
  assert.equal(s.canCancel, false);
  assert.equal(s.label, 'Fase: fase_do_futuro');
  assert.equal(phaseState(undefined).phase, 'idle');
});

// ---------------------------------------------------------------------
// Discos (1.12)
// ---------------------------------------------------------------------

test('o campo isWSL do servidor tem prioridade sobre a heuristica', () => {
  assert.equal(driveIsWSL({ letter: 'C:\\', fileSystem: 'NTFS', isWSL: true }), true);
  assert.equal(driveIsWSL({ letter: '\\\\wsl$\\Ubuntu', fileSystem: '9P', isWSL: false }), false);
});

test('a heuristica de WSL usa sistema de arquivos 9P ou caminho \\\\wsl', () => {
  assert.equal(driveIsWSL({ letter: 'Z:\\', fileSystem: '9P' }), true);
  assert.equal(driveIsWSL({ letter: '\\\\wsl$\\Ubuntu\\', fileSystem: '' }), true);
  assert.equal(driveIsWSL({ letter: 'C:\\', fileSystem: 'NTFS' }), false);
  assert.equal(driveIsWSL(null), false);
});

test('WSL, rede e CD-ROM nao vem marcados por padrao', () => {
  assert.equal(driveDefaultSelected({ letter: 'Z:\\', fileSystem: '9P', driveType: 'Fixed (SSD/HDD)' }), false);
  assert.equal(driveDefaultSelected({ letter: 'N:\\', fileSystem: 'NTFS', driveType: 'Network' }), false);
  assert.equal(driveDefaultSelected({ letter: 'E:\\', fileSystem: 'CDFS', driveType: 'CD-ROM' }), false);
});

test('discos fixos e removiveis vem marcados por padrao', () => {
  assert.equal(driveDefaultSelected({ letter: 'C:\\', fileSystem: 'NTFS', driveType: 'Fixed (SSD/HDD)' }), true);
  assert.equal(driveDefaultSelected({ letter: 'F:\\', fileSystem: 'exFAT', driveType: 'Removable' }), true);
});

test('o campo defaultSelected do servidor tem prioridade', () => {
  assert.equal(driveDefaultSelected({ driveType: 'Network', defaultSelected: true }), true);
  assert.equal(driveDefaultSelected({ driveType: 'Fixed (SSD/HDD)', defaultSelected: false }), false);
});

// ---------------------------------------------------------------------
// Assistente (1.11)
// ---------------------------------------------------------------------

test('o provedor direct e apelido legado de quick', () => {
  assert.equal(normalizeProvider('direct'), 'quick');
  assert.equal(normalizeProvider('quick'), 'quick');
  assert.equal(normalizeProvider('openrouter'), 'openrouter');
  assert.equal(normalizeProvider('ollama'), 'ollama');
  assert.equal(normalizeProvider(''), 'ollama');
});

test('o roteador aparece como Comandos Rapidos (sem modelo)', () => {
  assert.equal(providerLabel('quick'), 'Comandos Rápidos (sem modelo)');
  assert.equal(providerLabel('direct'), 'Comandos Rápidos (sem modelo)');
  assert.equal(providerLabel('ollama'), 'Ollama Local');
});

test('normalizeModelCatalog aceita o array da secao 1.11', () => {
  const { models } = normalizeModelCatalog([
    { id: 'qwen3-vl:8b', name: 'Qwen3-VL 8B', provider: 'ollama', sizeGB: 6.0, vision: true, tools: true, installed: false, recommended: true, fitsMemory: true },
  ]);
  assert.equal(models.length, 1);
  assert.equal(models[0].id, 'qwen3-vl:8b');
  assert.equal(models[0].vision, true);
  assert.equal(models[0].tools, true);
  assert.equal(models[0].recommended, true);
  assert.equal(models[0].sizeGB, 6);
});

test('normalizeModelCatalog aceita o formato legado localModels', () => {
  const { models, ollamaOnline } = normalizeModelCatalog({
    ollamaOnline: true,
    localModels: [{ id: 'gemma3:12b', name: 'Gemma 3 12B', supportsVision: false, isInstalled: true, isPrimary: false }],
  });
  assert.equal(ollamaOnline, true);
  assert.equal(models[0].id, 'gemma3:12b');
  assert.equal(models[0].vision, false);
  assert.equal(models[0].installed, true);
  assert.equal(models[0].tools, false);
});

test('normalizeModelCatalog tolera resposta vazia', () => {
  assert.deepEqual(normalizeModelCatalog(null), { models: [], ollamaOnline: false });
  assert.deepEqual(normalizeModelCatalog({}).models, []);
});

// ---------------------------------------------------------------------
// Paginacao
// ---------------------------------------------------------------------

test('pageBounds calcula offset e limites da pagina', () => {
  const b = pageBounds(1000, 3, 100);
  assert.equal(b.offset, 200);
  assert.equal(b.firstItem, 201);
  assert.equal(b.lastItem, 300);
  assert.equal(b.totalPages, 10);
});

test('pageBounds prende a pagina dentro do intervalo valido', () => {
  assert.equal(pageBounds(250, 99, 100).page, 3);
  assert.equal(pageBounds(250, 0, 100).page, 1);
  assert.equal(pageBounds(250, -5, 100).page, 1);
});

test('pageBounds trata total zero', () => {
  const b = pageBounds(0, 1, 100);
  assert.equal(b.totalPages, 1);
  assert.equal(b.firstItem, 0);
  assert.equal(b.lastItem, 0);
});

test('pageBounds corrige tamanho de pagina invalido', () => {
  const b = pageBounds(10, 1, 0);
  assert.equal(b.limit, 1);
  assert.equal(b.totalPages, 10);
});
