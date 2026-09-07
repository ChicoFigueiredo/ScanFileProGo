// Executa ui/js/app.js de verdade contra um DOM mínimo e um servidor de
// mentira. Serve para pegar erro de referência, id que não existe no HTML e
// caminho de renderização quebrado sem precisar abrir o navegador.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { createRequire } from 'node:module';

const aqui = path.dirname(fileURLToPath(import.meta.url));
const raizUI = path.join(aqui, '..');
const require = createRequire(import.meta.url);

const htmlFonte = fs.readFileSync(path.join(raizUI, 'index.html'), 'utf8');
const idsDoHTML = new Set([...htmlFonte.matchAll(/\bid="([^"]+)"/g)].map((m) => m[1]));

// ---------------------------------------------------------------------
// DOM mínimo
// ---------------------------------------------------------------------

function criarClassList(el) {
  const classes = new Set();
  return {
    add: (...cs) => cs.forEach((c) => classes.add(c)),
    remove: (...cs) => cs.forEach((c) => classes.delete(c)),
    contains: (c) => classes.has(c),
    toggle: (c, force) => {
      const alvo = force === undefined ? !classes.has(c) : !!force;
      if (alvo) classes.add(c);
      else classes.delete(c);
      return alvo;
    },
    forEach: (fn) => Array.from(classes).forEach(fn),
    _set: classes,
    _el: el,
  };
}

function criarElemento(tag = 'div', id = '') {
  const attrs = new Map();
  const el = {
    tagName: String(tag).toUpperCase(),
    id,
    style: { setProperty() {}, removeProperty() {} },
    dataset: {},
    value: '',
    checked: false,
    disabled: false,
    title: '',
    options: [],
    children: [],
    innerHTML: '',
    textContent: '',
    hidden: false,
    _listeners: {},
    addEventListener(tipo, fn) {
      (this._listeners[tipo] = this._listeners[tipo] || []).push(fn);
    },
    removeEventListener() {},
    dispatch(tipo, ev) {
      (this._listeners[tipo] || []).forEach((fn) => fn(ev || { target: this, preventDefault() {} }));
    },
    setAttribute(k, v) { attrs.set(k, String(v)); },
    getAttribute(k) { return attrs.has(k) ? attrs.get(k) : null; },
    removeAttribute(k) { attrs.delete(k); },
    appendChild(filho) { this.children.push(filho); return filho; },
    remove() {},
    closest() { return criarElemento(); },
    focus() {},
    scrollIntoView() {},
    querySelector() { return null; },
    querySelectorAll() { return []; },
    getBoundingClientRect() { return { left: 0, top: 0, width: 800, height: 600 }; },
    getContext() { return criarContexto2D(); },
    setPointerCapture() {},
    releasePointerCapture() {},
    get clientWidth() { return 800; },
    get clientHeight() { return 600; },
    get scrollHeight() { return 0; },
    scrollTop: 0,
  };
  el.classList = criarClassList(el);
  return el;
}

function criarContexto2D() {
  const noop = () => {};
  return {
    setTransform: noop, clearRect: noop, fillRect: noop, strokeRect: noop,
    fillText: noop, save: noop, restore: noop, beginPath: noop, rect: noop, clip: noop,
    createRadialGradient: () => ({ addColorStop: noop }),
    fillStyle: '', strokeStyle: '', lineWidth: 1, font: '', textAlign: 'left',
    shadowColor: '', shadowBlur: 0, shadowOffsetX: 0, shadowOffsetY: 0,
  };
}

// ---------------------------------------------------------------------
// Servidor de mentira
// ---------------------------------------------------------------------

const chamadas = [];

const FIXTURES = {
  '/api/config': {
    theme: 'theme-ochre-dark',
    uiZoom: 100,
    workerThreads: 16,
    hashAlgorithm: 'xxhash',
    hashMode: 'smart',
    minFileSize: 1,
    autoSaveIntervalMinutes: 5,
    treemapDepth: 5,
    treemapColorMode: 'extension',
    treemapViewMode: 'split',
    aiProvider: 'ollama',
    aiOllamaEndpoint: 'http://127.0.0.1:11434',
    aiDryRunDefault: true,
    aiOpenRouterKey: '',
    hasOpenRouterKey: true,
    selectedRoots: ['C:\\', 'Z:\\'],
  },
  '/api/system/info': { numCPU: 16, threadOptions: [0, 4, 8, 16, 32, 64], maxThreads: 64, version: '1.0.0', port: 47321, elevated: false },
  '/api/system/privileges': { isElevated: false, hasBackupAccess: false, activeUser: 'chico' },
  '/api/system/memory': { allocMB: 120, sysMB: 300, systemTotalRAMMB: 131072, systemUsedRAMMB: 40960, systemPercent: 31 },
  '/api/drives': [
    { letter: 'C:\\', volumeLabel: 'Windows', fileSystem: 'NTFS', driveType: 'Fixed (SSD/HDD)', totalBytes: 1e12, freeBytes: 4e11, usedPercent: 60, isWSL: false, defaultSelected: true },
    { letter: 'Z:\\', volumeLabel: 'Ubuntu', fileSystem: '9P', driveType: 'Fixed (SSD/HDD)', totalBytes: 1e11, freeBytes: 5e10, usedPercent: 50, isWSL: true, defaultSelected: false },
    { letter: 'N:\\', volumeLabel: 'NAS <b>x</b>', fileSystem: 'NTFS', driveType: 'Network', totalBytes: 1e12, freeBytes: 1e11, usedPercent: 90, isWSL: false, defaultSelected: false },
  ],
  '/api/scan/status': {
    phase: 'completed', totalFilesScanned: 1000, totalDirsScanned: 100, totalBytesScanned: 1024000,
    skippedCount: 7, prehashCount: 42, phase1Workers: 32, phase2Workers: 16,
    duplicateGroupsCount: 3, duplicateFilesCount: 8, duplicateWastedBytes: 2048,
    duplicateFolderGroupsCount: 1, duplicateFoldersCount: 2, duplicateFolderWastedBytes: 512,
    isWatching: false, errorsCount: 0, elapsedTimeSec: 12,
    activeWorkers: [], recentFiles: [],
  },
  '/api/logs': [{ timestamp: '2026-09-02T10:00:00Z', op: 'CREATE', path: 'C:\\a<b>.txt', sizeDelta: 100 }],
  '/api/logs/skipped': [{ timestamp: '2026-09-02T10:00:00Z', path: 'C:\\proc', reason: 'pseudo-arquivo do WSL' }],
  '/api/cache/autosave/status': { exists: false },
  '/api/tree': [
    { path: 'C:\\', name: 'C:', totalSize: 900, totalAllocatedSize: 900, fileCount: 10, subDirCount: 2, modTime: 1700000000, createTime: 1600000000 },
  ],
  '/api/tree/files': { total: 0, offset: 0, limit: 100, files: [] },
  '/api/duplicates': { groups: [], totalGroups: 0, totalFiles: 0, wastedBytes: 0 },
  '/api/folders/duplicates': { groups: [], totalGroups: 0 },
  '/api/stats/extensions': [],
  '/api/stats/idle-files': { totalIdleFiles: 0, totalIdleBytes: 0, topFiles: [] },
  '/api/ai/models': [
    { id: 'qwen3-vl:8b', name: 'Qwen3-VL 8B', provider: 'ollama', sizeGB: 6, vision: true, tools: true, installed: false, recommended: true, fitsMemory: true },
    { id: 'gpt-oss:20b', name: 'GPT-OSS 20B', provider: 'ollama', sizeGB: 13, vision: false, tools: true, installed: true, recommended: false, fitsMemory: true },
  ],
};

function fetchDeMentira(url, opts = {}) {
  const semQuery = String(url).split('?')[0];
  chamadas.push({ url: String(url), method: (opts.method || 'GET').toUpperCase(), headers: opts.headers, body: opts.body });

  const corpo = Object.prototype.hasOwnProperty.call(FIXTURES, semQuery) ? FIXTURES[semQuery] : { status: 'ok' };
  const texto = JSON.stringify(corpo);
  return Promise.resolve({
    ok: true,
    status: 200,
    text: () => Promise.resolve(texto),
    json: () => Promise.resolve(corpo),
  });
}

// ---------------------------------------------------------------------
// Montagem do ambiente
// ---------------------------------------------------------------------

const elementos = new Map();
const erros = [];

function montarAmbiente() {
  const body = criarElemento('body');
  const metaToken = criarElemento('meta');
  metaToken.setAttribute('content', 'token-de-teste');

  const documento = {
    readyState: 'complete',
    body,
    addEventListener() {},
    createElement: (tag) => criarElemento(tag),
    getElementById(id) {
      if (!idsDoHTML.has(id)) return null; // ids que não existem no HTML voltam null, como no navegador
      if (!elementos.has(id)) elementos.set(id, criarElemento('div', id));
      return elementos.get(id);
    },
    querySelector(sel) {
      if (sel === 'meta[name="scanfile-token"]') return metaToken;
      return null;
    },
    querySelectorAll: () => [],
  };

  const janela = {
    devicePixelRatio: 1,
    addEventListener() {},
    setTimeout: (fn) => setTimeout(fn, 0),
    clearTimeout,
    setInterval: () => 0,
    clearInterval,
  };

  globalThis.window = janela;
  globalThis.document = documento;
  // O Node 24 já traz um `navigator` somente leitura; sobrescrevemos a
  // propriedade para expor o que a interface usa.
  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    writable: true,
    value: {
      hardwareConcurrency: 16,
      sendBeacon: () => true,
      clipboard: { writeText: () => Promise.resolve() },
    },
  });
  globalThis.localStorage = {
    _m: new Map(),
    getItem(k) { return this._m.has(k) ? this._m.get(k) : null; },
    setItem(k, v) { this._m.set(k, String(v)); },
    removeItem(k) { this._m.delete(k); },
  };
  globalThis.CSS = { escape: (s) => String(s).replace(/["\\]/g, '\\$&') };
  globalThis.EventSource = class {
    constructor(url) {
      this.url = url;
      this._l = {};
      globalThis.__ultimoEventSource = this;
    }
    addEventListener(t, fn) { (this._l[t] = this._l[t] || []).push(fn); }
    close() { this.closed = true; }
    emit(t, data) { (this._l[t] || []).forEach((fn) => fn({ data: JSON.stringify(data) })); }
  };
  globalThis.fetch = fetchDeMentira;
  // A sondagem lenta de memória usa setInterval; sem neutralizá-la o processo
  // do teste nunca encerraria.
  globalThis.setInterval = () => 0;

  console.error = (...args) => erros.push(args.map(String).join(' '));
  console.warn = () => {};
}

async function aguardar(ms = 30) {
  await new Promise((r) => setTimeout(r, ms));
}

montarAmbiente();
require(path.join(raizUI, 'js', 'core.js'));
require(path.join(raizUI, 'js', 'app.js'));

test('app.js carrega e inicializa sem erro de execução', async () => {
  await aguardar(80);
  assert.deepEqual(erros, [], 'init() registrou erros: ' + erros.join(' | '));
});

test('todo getElementById de app.js corresponde a um id do index.html', () => {
  const fonte = fs.readFileSync(path.join(raizUI, 'js', 'app.js'), 'utf8');
  const consultados = [...fonte.matchAll(/(?:getElementById|\$)\('([^']+)'\)/g)].map((m) => m[1]);
  // Estes quatro são criados pelo próprio JS antes de serem consultados.
  const dinamicos = new Set(['btnRetryDrives', 'diffSearchInput', 'folderDiffTableBody', 'folderDiffPaginationBar']);
  const mortos = [...new Set(consultados)].filter((id) => !idsDoHTML.has(id) && !dinamicos.has(id));
  assert.deepEqual(mortos, [], 'ids inexistentes em index.html: ' + mortos.join(', '));
});

test('toda chamada de API leva o token da Sessão', () => {
  const semToken = chamadas.filter((c) => {
    if (c.url.startsWith('/api/events') || c.url.startsWith('/api/ui/closed')) return false;
    const h = c.headers;
    const valor = h && typeof h.get === 'function' ? h.get('X-ScanFile-Token') : null;
    return valor !== 'token-de-teste';
  });
  assert.equal(semToken.length, 0, 'chamadas sem token: ' + semToken.map((c) => c.url).join(', '));
});

test('o EventSource é aberto com o token na query', () => {
  const es = globalThis.__ultimoEventSource;
  assert.ok(es, 'nenhum EventSource foi aberto');
  assert.match(es.url, /^\/api\/events\?token=token-de-teste$/);
});

test('a interface ressincroniza o estado ao carregar e a cada onopen', async () => {
  const antes = chamadas.filter((c) => c.url === '/api/scan/status').length;
  assert.ok(antes >= 1, 'GET /api/scan/status não foi chamado ao carregar');

  globalThis.__ultimoEventSource.onopen();
  await aguardar(30);

  const depois = chamadas.filter((c) => c.url === '/api/scan/status').length;
  assert.ok(depois > antes, 'onopen do EventSource não ressincronizou o status');
});

test('o combo de threads é montado a partir de /api/system/info', () => {
  const select = elementos.get('workerThreads');
  assert.ok(select, 'o combo de threads não foi tocado');
  assert.equal(select.children.length, 6);
  assert.equal(select.children[0].textContent, 'Auto (32 na Fase 1, 16 na Fase 2)');
  assert.equal(select.children[1].textContent, '4 threads');
  assert.equal(select.children[5].textContent, '64 threads');
});

test('o disco do WSL fica desmarcado e ganha aviso; o rótulo do volume é escapado', () => {
  const grid = elementos.get('drivesGrid');
  assert.ok(grid.innerHTML.includes('drive-wsl'), 'o cartão do WSL não recebeu a classe de aviso');
  assert.ok(grid.innerHTML.includes('Volume do WSL'), 'faltou o aviso de WSL');
  assert.ok(grid.innerHTML.includes('NAS &lt;b&gt;x&lt;/b&gt;'), 'o rótulo do volume não foi escapado');
  assert.ok(!grid.innerHTML.includes('NAS <b>x</b>'), 'HTML do rótulo do volume vazou para a página');

  // A raiz Z:\ vinha marcada na Configuração salva, mas é WSL: sai da seleção.
  const cartaoZ = grid.innerHTML.split('data-letter="Z:\\"')[0].split('<div class="drive-card').pop();
  assert.ok(!cartaoZ.includes('selected'), 'o disco do WSL continuou marcado');
});

test('os eventos do Monitor e os Itens Pulados são renderizados escapados', async () => {
  const tabela = elementos.get('eventLogsTableBody');
  assert.ok(tabela.innerHTML.includes('Criado'), 'a operação não foi traduzida');
  assert.ok(tabela.innerHTML.includes('a&lt;b&gt;.txt'), 'o caminho do evento não foi escapado');

  const pulados = elementos.get('skippedItemsTableBody');
  assert.ok(pulados.innerHTML.includes('pseudo-arquivo do WSL'), 'o motivo do item pulado não apareceu');
  assert.equal(elementos.get('skippedCountBadge').textContent, '1 itens');
});

test('addFSEvent pelo SSE entra na lista do Monitor', async () => {
  const antes = elementos.get('eventLogsTableBody').innerHTML;
  globalThis.__ultimoEventSource.emit('fs_event', {
    timestamp: '2026-09-02T11:00:00Z', op: 'REMOVE', path: 'C:\\removido.bin', sizeDelta: -2048,
  });
  await aguardar(10);
  const depois = elementos.get('eventLogsTableBody').innerHTML;
  assert.notEqual(antes, depois, 'o evento do SSE não chegou à tabela');
  assert.ok(depois.includes('removido.bin'));
  assert.ok(depois.includes('Removido'));
});

test('o HUD mostra Itens Pulados, Pré-hash e as threads efetivas', () => {
  assert.equal(elementos.get('statSkipped').textContent, '7');
  assert.equal(elementos.get('statPrehash').textContent, '42');
  assert.equal(elementos.get('hudWorkersBadge').textContent, 'Threads: 32 na Fase 1, 16 na Fase 2');
});

test('a fase cancelling mantém o botão Cancelar e cancelled o esconde', async () => {
  const es = globalThis.__ultimoEventSource;
  const cancelar = elementos.get('btnCancelScan');
  const iniciar = elementos.get('btnStartScan');

  es.emit('scan_progress', { phase: 'cancelling', activeWorkers: [], recentFiles: [] });
  await aguardar(10);
  assert.equal(cancelar.classList.contains('hidden'), false, 'Cancelar sumiu durante cancelling');
  assert.equal(iniciar.classList.contains('hidden'), true, 'Iniciar reapareceu durante cancelling');

  es.emit('scan_progress', { phase: 'cancelled', activeWorkers: [], recentFiles: [] });
  await aguardar(10);
  assert.equal(cancelar.classList.contains('hidden'), true, 'Cancelar continuou visível após cancelled');
  assert.equal(iniciar.classList.contains('hidden'), false, 'Iniciar não voltou após cancelled');
  assert.equal(elementos.get('scanPhaseText').textContent, 'Varredura cancelada');
});

test('a fase indexing é reconhecida e bloqueia nova Varredura', async () => {
  globalThis.__ultimoEventSource.emit('scan_progress', { phase: 'indexing', activeWorkers: [], recentFiles: [] });
  await aguardar(10);
  assert.equal(elementos.get('scanPhaseText').textContent, 'Indexando duplicados');
  assert.equal(elementos.get('btnStartScan').classList.contains('hidden'), true);
  assert.equal(elementos.get('btnCancelScan').classList.contains('hidden'), false);
});

test('o POST de Configuração só carrega as chaves alteradas', async () => {
  const antes = chamadas.length;
  elementos.get('hashAlgo').value = 'blake3';
  elementos.get('hashAlgo').dispatch('change', { target: elementos.get('hashAlgo') });
  await aguardar(400);

  const posts = chamadas.slice(antes).filter((c) => c.url === '/api/config' && c.method === 'POST');
  assert.equal(posts.length, 1, 'esperava exatamente um POST de Configuração');
  const corpo = JSON.parse(posts[0].body);
  assert.deepEqual(corpo, { hashAlgorithm: 'blake3' });
  assert.ok(!('aiOpenRouterKey' in corpo), 'a chave do OpenRouter não pode subir sem ser digitada');
  assert.ok(!('aiProvider' in corpo), 'campos de IA não podem ser reenviados');
});

test('a tabela de arquivos da árvore é paginada de 100 em 100 pelo servidor', async () => {
  // Uma pasta com 250 arquivos: /api/tree traz no máximo os 500 maiores e o
  // total real em fileCount; a tabela pagina por /api/tree/files (contrato 1.4).
  FIXTURES['/api/tree'] = {
    path: 'C:\Dados', name: 'Dados', totalSize: 5000, fileCount: 250, subDirCount: 1,
    subDirs: [{ path: 'C:\Dados\sub', name: 'sub', totalSize: 100, fileCount: 1, subDirCount: 0 }],
    files: [],
  };
  FIXTURES['/api/tree/files'] = {
    total: 250, offset: 0, limit: 100,
    files: [{ path: 'C:\Dados\<script>.bin', name: '<script>.bin', size: 10, modTime: 1700000000, createTime: 1600000000 }],
  };

  const antes = chamadas.length;
  elementos.get('btnTreeRefresh').dispatch('click');
  await aguardar(60);

  const pedidos = chamadas.slice(antes).filter((c) => c.url.startsWith('/api/tree/files'));
  assert.equal(pedidos.length, 1, 'a tabela não chamou /api/tree/files');
  assert.match(pedidos[0].url, /offset=0/);
  assert.match(pedidos[0].url, /limit=100/);
  assert.match(pedidos[0].url, /sortBy=size_desc/);

  const barra = elementos.get('treePaginationBar');
  assert.ok(barra.innerHTML.includes('arquivos'), 'a barra de paginação de arquivos não foi montada');

  const corpo = elementos.get('treeTableBody');
  assert.ok(corpo.innerHTML.includes('&lt;script&gt;.bin'), 'o nome do arquivo não foi escapado');
  assert.ok(!corpo.innerHTML.includes('<script>'), 'HTML do nome do arquivo vazou para a página');
});

test('mudar a ordenação recarrega a primeira página de arquivos', async () => {
  const antes = chamadas.length;
  const ordenacao = elementos.get('treeFilesSortBy');
  ordenacao.value = 'mod_desc';
  ordenacao.dispatch('change', { target: ordenacao });
  await aguardar(40);

  const pedidos = chamadas.slice(antes).filter((c) => c.url.startsWith('/api/tree/files'));
  assert.equal(pedidos.length, 1);
  assert.match(pedidos[0].url, /sortBy=mod_desc/);
  assert.match(pedidos[0].url, /offset=0/);
});

test('401 dispara o aviso bloqueante de Sessão inválida', async () => {
  const original = globalThis.fetch;
  globalThis.fetch = () => Promise.resolve({
    ok: false, status: 401,
    text: () => Promise.resolve('{"error":"unauthorized"}'),
  });

  elementos.get('btnRefreshDrives').dispatch('click');
  await aguardar(50);

  assert.equal(elementos.get('sessionInvalidOverlay').classList.contains('hidden'), false,
    'o aviso de Sessão inválida não apareceu');
  globalThis.fetch = original;
});
