import test from 'node:test';
import assert from 'node:assert/strict';
import core from '../js/core.js';

const { selectDuplicatesByStrategy, validateFolderConfirm, validateDeleteConfirm, summarizeItemResults, DELETE_CONFIRM_WORD } = core;

const grupos = [
  {
    id: 'g1',
    files: [
      { path: 'C:\\a\\antigo.bin', size: 100, modTime: 1000 },
      { path: 'C:\\b\\novo.bin', size: 100, modTime: 3000 },
      { path: 'C:\\c\\meio.bin', size: 100, modTime: 2000 },
    ],
  },
  {
    id: 'g2',
    files: [
      { path: 'D:\\x\\um.iso', size: 500, modTime: 5000 },
      { path: 'D:\\y\\dois.iso', size: 500, modTime: 4000 },
    ],
  },
];

test('manter mais recente preserva exatamente uma copia por grupo', () => {
  const r = selectDuplicatesByStrategy(grupos, 'keep_newest');
  assert.deepEqual(r.kept, ['C:\\b\\novo.bin', 'D:\\x\\um.iso']);
  assert.equal(r.selected.length, 3);
  // Nenhum arquivo mantido pode aparecer na lista de remocao.
  const marcados = new Set(r.selected.map((f) => f.path));
  r.kept.forEach((p) => assert.ok(!marcados.has(p), `${p} foi mantido e marcado ao mesmo tempo`));
  // Cada grupo perde exatamente (n - 1) copias.
  assert.equal(marcados.size, grupos.reduce((acc, g) => acc + g.files.length - 1, 0));
});

test('manter mais antigo preserva a copia de menor modTime', () => {
  const r = selectDuplicatesByStrategy(grupos, 'keep_oldest');
  assert.deepEqual(r.kept, ['C:\\a\\antigo.bin', 'D:\\y\\dois.iso']);
  assert.equal(r.selected.length, 3);
});

test('a estrategia nao depende da ordem em que o servidor devolveu os arquivos', () => {
  const desordenado = [{ id: 'g', files: [...grupos[0].files].reverse() }];
  const r = selectDuplicatesByStrategy(desordenado, 'keep_newest');
  assert.deepEqual(r.kept, ['C:\\b\\novo.bin']);
});

test('a selecao carrega o tamanho de cada arquivo', () => {
  const r = selectDuplicatesByStrategy(grupos, 'keep_newest');
  const total = r.selected.reduce((acc, f) => acc + f.size, 0);
  assert.equal(total, 100 + 100 + 500);
});

test('grupos com menos de duas copias sao ignorados', () => {
  const r = selectDuplicatesByStrategy([{ id: 'só', files: [{ path: 'C:\\unico', size: 1, modTime: 1 }] }], 'keep_newest');
  assert.deepEqual(r.selected, []);
  assert.deepEqual(r.kept, []);
});

test('entradas invalidas nao quebram a selecao', () => {
  assert.deepEqual(selectDuplicatesByStrategy(null, 'keep_newest'), { selected: [], kept: [] });
  assert.deepEqual(selectDuplicatesByStrategy([{}], 'keep_newest'), { selected: [], kept: [] });
});

test('empate de modTime ainda preserva uma copia', () => {
  const empate = [{ id: 'e', files: [
    { path: 'C:\\1', size: 10, modTime: 7 },
    { path: 'C:\\2', size: 10, modTime: 7 },
    { path: 'C:\\3', size: 10, modTime: 7 },
  ] }];
  const r = selectDuplicatesByStrategy(empate, 'keep_newest');
  assert.equal(r.kept.length, 1);
  assert.equal(r.selected.length, 2);
});

// ---------------------------------------------------------------------
// Confirmacoes (1.5)
// ---------------------------------------------------------------------

test('reciclar pasta exige o nome base digitado', () => {
  const ok = validateFolderConfirm('D:\\Backup\\Fotos 2019', 'Fotos 2019');
  assert.equal(ok.ok, true);
  assert.equal(ok.confirmName, 'Fotos 2019');
});

test('reciclar pasta aceita espacos em volta e ignora caixa', () => {
  assert.equal(validateFolderConfirm('D:\\Backup\\Fotos', '  fotos  ').ok, true);
  assert.equal(validateFolderConfirm('D:\\Backup\\Fotos', 'FOTOS').confirmName, 'Fotos');
});

test('reciclar pasta recusa nome vazio ou diferente', () => {
  const vazio = validateFolderConfirm('D:\\Backup\\Fotos', '   ');
  assert.equal(vazio.ok, false);
  assert.match(vazio.reason, /Digite o nome/);

  const errado = validateFolderConfirm('D:\\Backup\\Fotos', 'Fotos2');
  assert.equal(errado.ok, false);
  assert.equal(errado.confirmName, '');
  assert.match(errado.reason, /não corresponde/);
});

test('reciclar pasta recusa alvo sem nome identificavel', () => {
  assert.equal(validateFolderConfirm('', 'qualquer').ok, false);
});

test('exclusao permanente exige a palavra EXCLUIR', () => {
  assert.equal(DELETE_CONFIRM_WORD, 'EXCLUIR');
  const ok = validateDeleteConfirm('EXCLUIR');
  assert.equal(ok.ok, true);
  assert.equal(ok.confirmText, 'EXCLUIR');
  // O que sobe para o servidor e sempre a palavra canonica.
  assert.equal(validateDeleteConfirm(' excluir ').confirmText, 'EXCLUIR');
});

test('exclusao permanente recusa qualquer outro texto', () => {
  ['', 'apagar', 'EXCLUI', 'EXCLUIRR', 'sim'].forEach((t) => {
    const r = validateDeleteConfirm(t);
    assert.equal(r.ok, false, `deveria recusar "${t}"`);
    assert.equal(r.confirmText, '');
  });
  assert.equal(validateDeleteConfirm(null).ok, false);
});

test('summarizeItemResults conta reciclados, recusados e falhas', () => {
  const r = summarizeItemResults({
    items: [
      { path: 'C:\\1', status: 'recycled' },
      { path: 'C:\\Windows', status: 'refused', reason: 'Pasta Protegida' },
      { path: 'E:\\x', status: 'refused', reason: 'volume sem Lixeira' },
      { path: 'C:\\2', status: 'failed', reason: 'arquivo em uso' },
    ],
    freedBytes: 4096,
  });
  assert.equal(r.counts.recycled, 1);
  assert.equal(r.counts.refused, 2);
  assert.equal(r.counts.failed, 1);
  assert.equal(r.okCount, 1);
  assert.equal(r.freedBytes, 4096);
});

test('summarizeItemResults trata resposta ausente ou sem items', () => {
  const vazio = summarizeItemResults(null);
  assert.deepEqual(vazio.items, []);
  assert.equal(vazio.okCount, 0);
  assert.equal(vazio.freedBytes, 0);

  const desconhecido = summarizeItemResults({ items: [{ path: 'C:\\1', status: 'wat' }] });
  assert.equal(desconhecido.counts.failed, 1);
});

test('summarizeItemResults soma exclusoes permanentes', () => {
  const r = summarizeItemResults({ items: [{ path: 'C:\\1', status: 'deleted' }], freedBytes: 10 });
  assert.equal(r.okCount, 1);
  assert.equal(r.counts.deleted, 1);
});
