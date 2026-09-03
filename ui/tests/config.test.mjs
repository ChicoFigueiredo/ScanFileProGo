import test from 'node:test';
import assert from 'node:assert/strict';
import core from '../js/core.js';

const { diffConfig, hasChanges } = core;

test('diffConfig devolve apenas as chaves alteradas', () => {
  const base = { theme: 'theme-ochre-dark', uiZoom: 100, workerThreads: 8 };
  const patch = diffConfig(base, { theme: 'theme-light-snow', uiZoom: 100, workerThreads: 8 });
  assert.deepEqual(patch, { theme: 'theme-light-snow' });
});

test('diffConfig devolve objeto vazio quando nada mudou', () => {
  const base = { a: 1, b: 'x' };
  assert.deepEqual(diffConfig(base, { a: 1, b: 'x' }), {});
  assert.equal(hasChanges(diffConfig(base, { a: 1 })), false);
});

test('diffConfig nunca inclui chaves ausentes no patch (C3: preserva ai*)', () => {
  const servidor = {
    theme: 'theme-ochre-dark',
    aiProvider: 'ollama',
    aiOllamaEndpoint: 'http://127.0.0.1:11434',
    aiDryRunDefault: true,
    hasOpenRouterKey: true,
  };
  const patch = diffConfig(servidor, { uiZoom: 120 });
  assert.deepEqual(patch, { uiZoom: 120 });
  assert.ok(!('aiProvider' in patch));
  assert.ok(!('aiOllamaEndpoint' in patch));
  assert.ok(!('aiDryRunDefault' in patch));
});

test('diffConfig compara arrays elemento a elemento', () => {
  const base = { selectedRoots: ['C:\\', 'D:\\'] };
  assert.deepEqual(diffConfig(base, { selectedRoots: ['C:\\', 'D:\\'] }), {});
  assert.deepEqual(diffConfig(base, { selectedRoots: ['C:\\'] }), { selectedRoots: ['C:\\'] });
  assert.deepEqual(diffConfig(base, { selectedRoots: ['D:\\', 'C:\\'] }), { selectedRoots: ['D:\\', 'C:\\'] });
});

test('diffConfig trata base ausente enviando tudo que foi informado', () => {
  const patch = diffConfig(null, { uiZoom: 120 });
  assert.deepEqual(patch, { uiZoom: 120 });
});

test('diffConfig ignora valores indefinidos', () => {
  const patch = diffConfig({ a: 1 }, { a: 2, b: undefined });
  assert.deepEqual(patch, { a: 2 });
});

test('diffConfig inclui chave nova ainda que o valor seja falso', () => {
  const patch = diffConfig({}, { chkFolderTopLevelOnly: false });
  assert.deepEqual(patch, { chkFolderTopLevelOnly: false });
  assert.equal(hasChanges(patch), true);
});

test('a chave do OpenRouter so entra no patch quando digitada', () => {
  // O GET nunca devolve a chave; o campo do formulario vem vazio.
  const doServidor = { aiOpenRouterKey: '', hasOpenRouterKey: true };
  const semDigitar = diffConfig(doServidor, { aiOllamaEndpoint: 'http://127.0.0.1:11434' });
  assert.ok(!('aiOpenRouterKey' in semDigitar));

  const digitando = diffConfig(doServidor, { aiOpenRouterKey: 'sk-or-v1-abc' });
  assert.deepEqual(digitando, { aiOpenRouterKey: 'sk-or-v1-abc' });
});

test('hasChanges distingue patch vazio de patch preenchido', () => {
  assert.equal(hasChanges({}), false);
  assert.equal(hasChanges(null), false);
  assert.equal(hasChanges({ a: 1 }), true);
});
