/*
 * ScanFile Pro - Núcleo de funções puras compartilhadas.
 *
 * Este arquivo é servido diretamente ao navegador (expõe `window.ScanFileCore`)
 * e também é carregado pelos testes no Node (`module.exports`). Por isso NÃO usa
 * `import`/`export` de ESM nem qualquer API exclusiva de um dos ambientes.
 */
(function (globalScope, factory) {
  'use strict';
  var api = factory();
  if (typeof module === 'object' && module && typeof module.exports === 'object') {
    module.exports = api;
  }
  if (globalScope) {
    globalScope.ScanFileCore = api;
  }
})(typeof window !== 'undefined' ? window : (typeof globalThis !== 'undefined' ? globalThis : null), function () {
  'use strict';

  // =====================================================================
  // Escape de HTML
  // =====================================================================

  var ESCAPE_MAP = {
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;',
  };

  /**
   * Escapa texto para interpolação segura em HTML, incluindo contexto de atributo.
   * Todo dado vindo do servidor, do disco ou do modelo precisa passar por aqui.
   * A crase não é escapada de propósito: todo atributo gerado aqui usa aspas
   * duplas (já escapadas) e `renderMarkdown` precisa reconhecer blocos ``` .
   */
  function esc(value) {
    if (value === null || value === undefined) return '';
    return String(value).replace(/[&<>"']/g, function (ch) {
      return ESCAPE_MAP[ch];
    });
  }

  /**
   * Interpola valores escapados em um template literal marcado.
   * Uso: html`<div title="${caminho}">${nome}</div>`
   */
  function html(strings) {
    var out = strings[0];
    for (var i = 1; i < arguments.length; i++) {
      out += esc(arguments[i]) + strings[i];
    }
    return out;
  }

  // =====================================================================
  // Formatação
  // =====================================================================

  var BYTE_UNITS = ['B', 'KB', 'MB', 'GB', 'TB', 'PB', 'EB'];

  function formatBytes(bytes, decimals) {
    var n = Number(bytes);
    if (!Number.isFinite(n) || n === 0) return '0 B';
    var dm = decimals === undefined || decimals < 0 ? 2 : decimals;
    var neg = n < 0;
    var abs = Math.abs(n);
    var i = Math.floor(Math.log(abs) / Math.log(1024));
    if (i < 0) i = 0;
    if (i >= BYTE_UNITS.length) i = BYTE_UNITS.length - 1;
    var val = (abs / Math.pow(1024, i)).toFixed(i === 0 ? 0 : dm);
    return (neg ? '-' : '') + val + ' ' + BYTE_UNITS[i];
  }

  function formatNumber(num) {
    if (num === null || num === undefined || num === '') return '0';
    var n = Number(num);
    if (!Number.isFinite(n)) return '0';
    return n.toLocaleString('pt-BR');
  }

  function formatDate(unixSec) {
    if (!unixSec) return '-';
    var d = new Date(Number(unixSec) * 1000);
    if (isNaN(d.getTime())) return '-';
    return d.toLocaleString('pt-BR');
  }

  function formatTimeAgo(date, now) {
    var ref = now instanceof Date ? now : new Date();
    var d = date instanceof Date ? date : new Date(date);
    if (isNaN(d.getTime())) return 'em data desconhecida';
    var diff = Math.floor((ref.getTime() - d.getTime()) / 1000);
    if (diff < 60) return 'há poucos segundos';
    if (diff < 3600) return 'há ' + Math.floor(diff / 60) + ' min';
    if (diff < 86400) return 'há ' + Math.floor(diff / 3600) + ' horas';
    return 'em ' + d.toLocaleDateString('pt-BR');
  }

  function basename(path) {
    if (!path) return '';
    var clean = String(path).replace(/[\\/]+$/, '');
    var idx = Math.max(clean.lastIndexOf('\\'), clean.lastIndexOf('/'));
    if (idx < 0) return clean;
    return clean.slice(idx + 1) || clean;
  }

  function parentPath(path) {
    if (!path) return '';
    var clean = String(path).replace(/[\\/]+$/, '');
    var idx = Math.max(clean.lastIndexOf('\\'), clean.lastIndexOf('/'));
    if (idx < 0) return '';
    if (idx <= 2) return clean.slice(0, 3); // C:\
    return clean.slice(0, idx);
  }

  /** `JSON.parse` que nunca lança: devolve `fallback` em qualquer erro. */
  function safeParseJSON(text, fallback) {
    if (typeof text !== 'string' || text.trim() === '') return fallback;
    try {
      var parsed = JSON.parse(text);
      return parsed === null && fallback !== undefined ? fallback : parsed;
    } catch (e) {
      return fallback;
    }
  }

  // =====================================================================
  // Configuração: enviar só o que mudou (1.6 / C3 / M13)
  // =====================================================================

  function sameValue(a, b) {
    if (Array.isArray(a) && Array.isArray(b)) {
      if (a.length !== b.length) return false;
      for (var i = 0; i < a.length; i++) {
        if (!sameValue(a[i], b[i])) return false;
      }
      return true;
    }
    if (Array.isArray(a) || Array.isArray(b)) return false;
    return a === b;
  }

  /**
   * Devolve apenas as chaves de `next` cujo valor difere de `base`.
   * Chaves ausentes em `next` nunca entram no resultado, de modo que um POST
   * parcial jamais sobrescreve campos que a interface não conhece.
   */
  function diffConfig(base, next) {
    var patch = {};
    if (!next || typeof next !== 'object') return patch;
    var reference = base && typeof base === 'object' ? base : {};
    Object.keys(next).forEach(function (key) {
      var value = next[key];
      if (value === undefined) return;
      if (!Object.prototype.hasOwnProperty.call(reference, key) || !sameValue(reference[key], value)) {
        patch[key] = value;
      }
    });
    return patch;
  }

  /** Verdadeiro quando o patch tem ao menos uma chave para enviar. */
  function hasChanges(patch) {
    return !!patch && Object.keys(patch).length > 0;
  }

  // =====================================================================
  // Threads (1.3)
  // =====================================================================

  /** Potências de 2 de 4 até 4 × numCPU, com 0 (Auto) na frente. */
  function buildThreadOptions(numCPU) {
    var cpus = Number(numCPU);
    if (!Number.isFinite(cpus) || cpus < 1) cpus = 1;
    var max = 4 * cpus;
    var options = [0];
    for (var v = 4; v <= max; v *= 2) {
      options.push(v);
    }
    if (options.length === 1) options.push(4);
    return options;
  }

  /** Rótulo do combo de threads; 0 vira "Auto (N na Fase 1, M na Fase 2)". */
  function threadOptionLabel(value, numCPU) {
    var v = Number(value) || 0;
    var cpus = Number(numCPU);
    if (v === 0) {
      if (!Number.isFinite(cpus) || cpus < 1) return 'Auto (definido pelo servidor)';
      return 'Auto (' + (2 * cpus) + ' na Fase 1, ' + cpus + ' na Fase 2)';
    }
    return v + ' threads';
  }

  // =====================================================================
  // Fases da Varredura (1.2)
  // =====================================================================

  var PHASES = {
    idle: { label: 'Ocioso', badge: 'idle', busy: false, canStart: true, canCancel: false },
    loading_cache: { label: 'Carregando Snapshot', badge: 'scanning', busy: true, canStart: false, canCancel: false },
    phase1_metadata: { label: 'Fase 1: Mapeamento de Metadados', badge: 'scanning', busy: true, canStart: false, canCancel: true },
    phase2_hashing: { label: 'Fase 2: Cálculo de Hashes', badge: 'scanning', busy: true, canStart: false, canCancel: true },
    indexing: { label: 'Indexando duplicados', badge: 'scanning', busy: true, canStart: false, canCancel: true },
    cancelling: { label: 'Cancelando a Varredura...', badge: 'scanning', busy: true, canStart: false, canCancel: true },
    cancelled: { label: 'Varredura cancelada', badge: 'idle', busy: false, canStart: true, canCancel: false },
    completed: { label: 'Varredura concluída', badge: 'live', busy: false, canStart: true, canCancel: false },
    watching: { label: 'Monitoramento ativo', badge: 'live', busy: false, canStart: true, canCancel: false },
  };

  /** Estado derivado de uma fase; fases desconhecidas caem num padrão seguro. */
  function phaseState(phase) {
    var key = typeof phase === 'string' ? phase : '';
    var found = PHASES[key];
    if (!found) {
      return { phase: key || 'idle', label: key ? 'Fase: ' + key : 'Ocioso', badge: 'idle', busy: false, canStart: true, canCancel: false };
    }
    return {
      phase: key,
      label: found.label,
      badge: found.badge,
      busy: found.busy,
      canStart: found.canStart,
      canCancel: found.canCancel,
    };
  }

  // =====================================================================
  // Treemap: squarify (Bruls, Huizing, van Wijk)
  // =====================================================================

  function rowWorstAspect(children, start, count, rowSum, side, remSize, totalLength) {
    if (count <= 0 || side <= 0 || remSize <= 0 || totalLength <= 0 || rowSum <= 0) return Infinity;
    var rowThickness = (rowSum / remSize) * totalLength;
    if (rowThickness <= 0) return Infinity;
    var worst = 0;
    for (var i = 0; i < count; i++) {
      var size = children[start + i].totalSize;
      var itemLen = (size / rowSum) * side;
      if (itemLen <= 0) return Infinity;
      var ratio = Math.max(itemLen / rowThickness, rowThickness / itemLen);
      if (ratio > worst) worst = ratio;
    }
    return worst;
  }

  /**
   * Distribui `children` (ordenados do maior para o menor) no retângulo dado.
   * A área total é preservada e os retângulos não se sobrepõem.
   * Percorre a lista por índice, sem `slice`/`concat`, para ser linear (H6).
   */
  function squarify(children, totalSize, x, y, width, height) {
    var rects = [];
    if (!children || children.length === 0 || width <= 0 || height <= 0 || totalSize <= 0) return rects;

    var n = children.length;
    var idx = 0;
    var remSize = totalSize;
    var rx = x;
    var ry = y;
    var rw = width;
    var rh = height;

    while (idx < n) {
      if (rw <= 0 || rh <= 0 || remSize <= 0) break;

      var isHorizontal = rw >= rh;
      var side = isHorizontal ? rh : rw;            // comprimento da faixa
      var totalLength = isHorizontal ? rw : rh;     // dimensão que a faixa consome

      var rowCount = 1;
      var rowSum = children[idx].totalSize;
      var bestWorst = rowWorstAspect(children, idx, rowCount, rowSum, side, remSize, totalLength);

      while (idx + rowCount < n) {
        var nextSum = rowSum + children[idx + rowCount].totalSize;
        var testWorst = rowWorstAspect(children, idx, rowCount + 1, nextSum, side, remSize, totalLength);
        if (testWorst <= bestWorst) {
          bestWorst = testWorst;
          rowSum = nextSum;
          rowCount++;
        } else {
          break;
        }
      }

      var stripThickness = (rowSum / remSize) * totalLength;
      var offset = 0;
      for (var j = 0; j < rowCount; j++) {
        var item = children[idx + j];
        var itemLen = rowSum > 0 ? (item.totalSize / rowSum) * side : 0;
        if (isHorizontal) {
          rects.push({ x: rx, y: ry + offset, w: stripThickness, h: itemLen, node: item });
        } else {
          rects.push({ x: rx + offset, y: ry, w: itemLen, h: stripThickness, node: item });
        }
        offset += itemLen;
      }

      if (isHorizontal) {
        rx += stripThickness;
        rw -= stripThickness;
      } else {
        ry += stripThickness;
        rh -= stripThickness;
      }

      remSize -= rowSum;
      idx += rowCount;
    }

    return rects;
  }

  // =====================================================================
  // Treemap: mapeamento de coordenadas com zoom da interface
  // =====================================================================

  /**
   * Converte a posição do ponteiro (coordenadas de cliente, já afetadas pelo
   * zoom da página) para as coordenadas de layout em que o treemap foi
   * desenhado. `rect` é o `getBoundingClientRect()` do canvas (escalado pelo
   * zoom) e `layoutWidth`/`layoutHeight` são as medidas de layout usadas no
   * cálculo do treemap (`clientWidth`/`clientHeight`, sem zoom).
   *
   * Com zoom 100% a razão é 1 e nada muda; com 80% ou 120% a razão corrige a
   * diferença, de modo que clique e desenho coincidam (item 13 do contrato).
   */
  function mapClientToCanvas(clientX, clientY, rect, layoutWidth, layoutHeight) {
    if (!rect) return { x: 0, y: 0 };
    var visualW = Number(rect.width) || 0;
    var visualH = Number(rect.height) || 0;
    var lw = Number(layoutWidth);
    var lh = Number(layoutHeight);
    if (!Number.isFinite(lw) || lw <= 0) lw = visualW;
    if (!Number.isFinite(lh) || lh <= 0) lh = visualH;
    var sx = visualW > 0 ? lw / visualW : 1;
    var sy = visualH > 0 ? lh / visualH : 1;
    return {
      x: (Number(clientX) - Number(rect.left || 0)) * sx,
      y: (Number(clientY) - Number(rect.top || 0)) * sy,
    };
  }

  /** Fator de escala visual aplicado pelo zoom da página a um elemento. */
  function zoomScaleOf(rect, layoutWidth) {
    var lw = Number(layoutWidth);
    var visualW = rect ? Number(rect.width) : 0;
    if (!Number.isFinite(lw) || lw <= 0 || !Number.isFinite(visualW) || visualW <= 0) return 1;
    return visualW / lw;
  }

  /** Último (mais profundo) retângulo que contém o ponto, ou null. */
  function hitTest(nodes, x, y) {
    if (!nodes || nodes.length === 0) return null;
    for (var i = nodes.length - 1; i >= 0; i--) {
      var n = nodes[i];
      if (x >= n.x && x <= n.x + n.w && y >= n.y && y <= n.y + n.h) return n;
    }
    return null;
  }

  // =====================================================================
  // Seleção de duplicados
  // =====================================================================

  /**
   * Marca para remoção todas as cópias de cada grupo exceto uma.
   * `strategy`: 'keep_newest' mantém a de maior `modTime`; 'keep_oldest' a de
   * menor. Não depende da ordem em que o servidor devolveu os arquivos.
   * Grupos com menos de 2 arquivos são ignorados por inteiro.
   */
  function selectDuplicatesByStrategy(groups, strategy) {
    var selected = [];
    var kept = [];
    if (!Array.isArray(groups)) return { selected: selected, kept: kept };
    var wantNewest = strategy !== 'keep_oldest';

    groups.forEach(function (group) {
      var files = group && Array.isArray(group.files) ? group.files : [];
      if (files.length < 2) return;

      var keepIdx = 0;
      for (var i = 1; i < files.length; i++) {
        var cur = Number(files[i].modTime) || 0;
        var best = Number(files[keepIdx].modTime) || 0;
        if (wantNewest ? cur > best : cur < best) keepIdx = i;
      }
      kept.push(files[keepIdx].path);
      for (var j = 0; j < files.length; j++) {
        if (j === keepIdx) continue;
        selected.push({ path: files[j].path, size: Number(files[j].size) || 0 });
      }
    });

    return { selected: selected, kept: kept };
  }

  // =====================================================================
  // Confirmações de Reciclagem e Exclusão Permanente (1.5)
  // =====================================================================

  var DELETE_CONFIRM_WORD = 'EXCLUIR';

  /**
   * Valida a confirmação digitada para reciclar uma pasta.
   * O usuário precisa digitar o nome base da pasta (comparação sem
   * diferenciar maiúsculas, como o sistema de arquivos do Windows).
   */
  function validateFolderConfirm(folderName, typed) {
    var expected = basename(folderName);
    var got = typeof typed === 'string' ? typed.trim() : '';
    if (!expected) return { ok: false, reason: 'Pasta sem nome identificável.', confirmName: '' };
    if (!got) return { ok: false, reason: 'Digite o nome da pasta para confirmar.', confirmName: '' };
    if (got.toLowerCase() !== expected.toLowerCase()) {
      return { ok: false, reason: 'O nome digitado não corresponde a "' + expected + '".', confirmName: '' };
    }
    return { ok: true, reason: '', confirmName: expected };
  }

  /** Valida o texto `EXCLUIR` da Exclusão Permanente. */
  function validateDeleteConfirm(typed) {
    var got = typeof typed === 'string' ? typed.trim() : '';
    if (got.toUpperCase() !== DELETE_CONFIRM_WORD) {
      return { ok: false, reason: 'Digite ' + DELETE_CONFIRM_WORD + ' para confirmar a exclusão permanente.', confirmText: '' };
    }
    return { ok: true, reason: '', confirmText: DELETE_CONFIRM_WORD };
  }

  /** Agrupa os resultados por item devolvidos por /api/files/recycle|delete. */
  function summarizeItemResults(result) {
    var items = result && Array.isArray(result.items) ? result.items : [];
    var counts = { recycled: 0, deleted: 0, refused: 0, failed: 0 };
    items.forEach(function (it) {
      var status = it && it.status ? String(it.status) : 'failed';
      if (Object.prototype.hasOwnProperty.call(counts, status)) counts[status]++;
      else counts.failed++;
    });
    return {
      items: items,
      counts: counts,
      okCount: counts.recycled + counts.deleted,
      freedBytes: result && Number(result.freedBytes) ? Number(result.freedBytes) : 0,
    };
  }

  // =====================================================================
  // Discos (1.12)
  // =====================================================================

  /** Heurística de WSL usada só como reserva quando o servidor não informa. */
  function driveIsWSL(drive) {
    if (!drive) return false;
    if (typeof drive.isWSL === 'boolean') return drive.isWSL;
    var fs = (drive.fileSystem || '').toString().toUpperCase();
    var letter = (drive.letter || '').toString().toLowerCase();
    return fs === '9P' || letter.indexOf('\\\\wsl') === 0;
  }

  /** Se o disco deve vir marcado; WSL, rede e CD-ROM ficam desmarcados. */
  function driveDefaultSelected(drive) {
    if (!drive) return false;
    if (typeof drive.defaultSelected === 'boolean') return drive.defaultSelected;
    if (driveIsWSL(drive)) return false;
    var type = (drive.driveType || '').toString().toLowerCase();
    if (type.indexOf('network') >= 0 || type.indexOf('rede') >= 0) return false;
    if (type.indexOf('cd-rom') >= 0 || type.indexOf('cdrom') >= 0) return false;
    return type.indexOf('fixed') >= 0 || type.indexOf('removable') >= 0;
  }

  // =====================================================================
  // Assistente (1.11)
  // =====================================================================

  var PROVIDER_LABELS = {
    ollama: 'Ollama Local',
    openrouter: 'OpenRouter (Nuvem)',
    quick: 'Comandos Rápidos (sem modelo)',
  };

  /** `direct` é apelido legado de `quick`. */
  function normalizeProvider(provider) {
    var p = (provider || '').toString().toLowerCase();
    if (p === 'direct' || p === 'quick') return 'quick';
    if (p === 'openrouter') return 'openrouter';
    return 'ollama';
  }

  function providerLabel(provider) {
    return PROVIDER_LABELS[normalizeProvider(provider)];
  }

  /**
   * Normaliza o catálogo de modelos para a forma da seção 1.11, aceitando
   * tanto o array novo quanto o objeto `{localModels:[...]}` legado.
   */
  function normalizeModelCatalog(payload) {
    var raw = [];
    var ollamaOnline = false;
    if (Array.isArray(payload)) {
      raw = payload;
    } else if (payload && typeof payload === 'object') {
      ollamaOnline = !!payload.ollamaOnline;
      if (Array.isArray(payload.models)) raw = payload.models;
      else if (Array.isArray(payload.localModels)) raw = payload.localModels;
    }
    var models = raw.map(function (m) {
      var sizeGB = m.sizeGB !== undefined ? Number(m.sizeGB) : 0;
      return {
        id: m.id || '',
        name: m.name || m.id || '',
        provider: normalizeProvider(m.provider || 'ollama'),
        sizeGB: Number.isFinite(sizeGB) ? sizeGB : 0,
        vision: m.vision !== undefined ? !!m.vision : !!m.supportsVision,
        tools: m.tools !== undefined ? !!m.tools : !!m.supportsTools,
        installed: m.installed !== undefined ? !!m.installed : !!m.isInstalled,
        recommended: m.recommended !== undefined ? !!m.recommended : !!m.isPrimary,
        fitsMemory: m.fitsMemory !== undefined ? !!m.fitsMemory : true,
      };
    });
    return { models: models, ollamaOnline: ollamaOnline };
  }

  // =====================================================================
  // Markdown mínimo e seguro para as respostas do Assistente (C5)
  // =====================================================================

  /**
   * Converte um subconjunto de Markdown em HTML. TODO o texto é escapado
   * primeiro, então nenhuma marcação vinda do modelo sobrevive.
   */
  function renderMarkdown(md) {
    if (!md) return '';
    var out = esc(md);
    out = out.replace(/```[a-zA-Z0-9]*\n([\s\S]*?)```/g, '<pre><code>$1</code></pre>');
    out = out.replace(/`([^`\n]+)`/g, '<code>$1</code>');
    out = out.replace(/\*\*([^*\n]+)\*\*/g, '<strong>$1</strong>');
    out = out.replace(/(^|[^*])\*([^*\n]+)\*/g, '$1<em>$2</em>');
    out = out.replace(/\n/g, '<br>');
    return out;
  }

  // =====================================================================
  // Paginação
  // =====================================================================

  function pageBounds(totalItems, page, pageSize) {
    var total = Math.max(0, Number(totalItems) || 0);
    var size = Math.max(1, Number(pageSize) || 1);
    var pages = Math.max(1, Math.ceil(total / size));
    var current = Math.min(Math.max(1, Number(page) || 1), pages);
    var offset = (current - 1) * size;
    return {
      page: current,
      totalPages: pages,
      offset: offset,
      limit: size,
      firstItem: total === 0 ? 0 : offset + 1,
      lastItem: Math.min(total, offset + size),
    };
  }

  return {
    esc: esc,
    html: html,
    formatBytes: formatBytes,
    formatNumber: formatNumber,
    formatDate: formatDate,
    formatTimeAgo: formatTimeAgo,
    basename: basename,
    parentPath: parentPath,
    safeParseJSON: safeParseJSON,
    diffConfig: diffConfig,
    hasChanges: hasChanges,
    buildThreadOptions: buildThreadOptions,
    threadOptionLabel: threadOptionLabel,
    phaseState: phaseState,
    squarify: squarify,
    mapClientToCanvas: mapClientToCanvas,
    zoomScaleOf: zoomScaleOf,
    hitTest: hitTest,
    selectDuplicatesByStrategy: selectDuplicatesByStrategy,
    validateFolderConfirm: validateFolderConfirm,
    validateDeleteConfirm: validateDeleteConfirm,
    summarizeItemResults: summarizeItemResults,
    driveIsWSL: driveIsWSL,
    driveDefaultSelected: driveDefaultSelected,
    normalizeProvider: normalizeProvider,
    providerLabel: providerLabel,
    normalizeModelCatalog: normalizeModelCatalog,
    renderMarkdown: renderMarkdown,
    pageBounds: pageBounds,
    DELETE_CONFIRM_WORD: DELETE_CONFIRM_WORD,
  };
});
