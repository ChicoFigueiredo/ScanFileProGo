// ScanFile Pro - Interface web.
//
// Regras desta camada:
//  - Toda chamada a /api/* passa por apiFetch(), que injeta o token da Sessão.
//  - Nenhum dado externo (caminhos, rótulos de volume, texto do modelo) entra
//    em innerHTML sem passar por esc().
//  - A Configuração é salva em patches: só as chaves que o usuário mudou.
(function () {
  'use strict';

  var C = window.ScanFileCore;
  if (!C) {
    document.addEventListener('DOMContentLoaded', function () {
      document.body.textContent = 'Falha ao carregar js/core.js. Reabra o ScanFile.';
    });
    return;
  }

  var esc = C.esc;
  var formatBytes = C.formatBytes;
  var formatNumber = C.formatNumber;
  var formatDate = C.formatDate;

  // ==========================================
  // SESSÃO: token, apiFetch e tratamento de 401 (contrato 1.1)
  // ==========================================

  var TOKEN_PLACEHOLDER = '{{SCANFILE_TOKEN}}';

  function readSessionToken() {
    var meta = document.querySelector('meta[name="scanfile-token"]');
    var value = meta ? (meta.getAttribute('content') || '').trim() : '';
    // Enquanto o servidor não substitui o placeholder, seguimos sem token.
    if (!value || value === TOKEN_PLACEHOLDER) return '';
    return value;
  }

  var sessionToken = '';
  var sessionInvalid = false;

  function tokenQuery(url) {
    if (!sessionToken) return url;
    return url + (url.indexOf('?') >= 0 ? '&' : '?') + 'token=' + encodeURIComponent(sessionToken);
  }

  function showSessionInvalid() {
    if (sessionInvalid) return;
    sessionInvalid = true;
    if (state.sseSource) {
      try { state.sseSource.close(); } catch (e) { /* já fechado */ }
      state.sseSource = null;
    }
    var overlay = document.getElementById('sessionInvalidOverlay');
    if (overlay) overlay.classList.remove('hidden');
  }

  /** fetch central: injeta X-ScanFile-Token e converte 401 em aviso bloqueante. */
  async function apiFetch(url, opts) {
    if (sessionInvalid) throw new Error('Sessão inválida');
    var options = Object.assign({}, opts || {});
    var headers = new Headers(options.headers || {});
    if (sessionToken) headers.set('X-ScanFile-Token', sessionToken);
    options.headers = headers;

    var res = await fetch(url, options);
    if (res.status === 401) {
      showSessionInvalid();
      throw new Error('Sessão inválida, reabra o ScanFile');
    }
    return res;
  }

  /** GET que devolve JSON já validado, ou lança com a mensagem do servidor. */
  async function apiGetJSON(url, opts) {
    var res = await apiFetch(url, opts);
    if (!res.ok) throw new Error(await readError(res));
    var text = await res.text();
    return C.safeParseJSON(text, null);
  }

  /** POST JSON; devolve { ok, status, data, error }. Nunca lança por status. */
  async function apiPostJSON(url, body) {
    var res = await apiFetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body === undefined ? {} : body),
    });
    var text = await res.text();
    var data = C.safeParseJSON(text, null);
    return {
      ok: res.ok,
      status: res.status,
      data: data,
      error: res.ok ? '' : (data && data.error ? String(data.error) : (text || ('HTTP ' + res.status))),
      raw: text,
    };
  }

  async function readError(res) {
    var text = '';
    try { text = await res.text(); } catch (e) { text = ''; }
    var data = C.safeParseJSON(text, null);
    if (data && data.error) return String(data.error);
    return text || ('HTTP ' + res.status);
  }

  // ==========================================
  // ESTADO
  // ==========================================

  var state = {
    drives: [],
    selectedRoots: new Set(),
    systemInfo: null,
    scanStatus: null,
    phase: 'idle',
    isScanning: false,
    currentTab: 'drivesTab',
    currentFolderSubtab: 'folderDupSubtab',
    treePath: '',
    treeData: null,
    treeFiles: [],
    treeFilesTotal: 0,
    treeFilesPage: 1,
    treeFilesLimit: 100,
    treeFilesSortBy: 'size_desc',
    treeFilesSupported: true,
    treemap: {
      depth: 5,
      colorMode: 'extension',
      viewMode: 'split',
      rawTree: null,
      layoutNodes: [],
      layoutWidth: 0,
      layoutHeight: 0,
      hoveredNode: null,
      selectedNode: null,
      contextNode: null,
    },
    duplicatesData: null,
    folderDuplicatesData: null,
    folderComparisonData: null,
    folderDiffFilter: 'ALL',
    dupPage: 1,
    dupLimit: 50,
    folderDupPage: 1,
    folderDupLimit: 50,
    idlePage: 1,
    idleLimit: 50,
    isLoadingDups: false,
    isLoadingFolderDups: false,
    selectedFilesForDelete: new Map(),
    selectedIdleFiles: new Map(),
    idleData: null,
    eventLogs: [],
    skippedItems: [],
    sseSource: null,
    lastProgressAt: 0,
    uiZoom: 100,
    theme: 'theme-ochre-dark',
    serverConfig: null,
    configLoaded: false,
    pendingConfigPatch: {},
    isBusyOperation: false,
    hasOpenRouterKey: false,
    ai: {
      provider: 'ollama',
      selectedModel: '',
      models: [],
      ollamaOnline: false,
      chatHistory: [],
      isGenerating: false,
      isPulling: false,
    },
    confirmAction: null,
  };

  // ==========================================
  // ELEMENTOS (somente ids que existem em index.html)
  // ==========================================

  function $(id) {
    return document.getElementById(id);
  }

  var elements = {
    themeSelector: $('themeSelector'),
    memAppUsage: $('memAppUsage'),
    memSysUsage: $('memSysUsage'),
    memoryBarFill: $('memoryBarFill'),
    treePaginationBar: $('treePaginationBar'),
    treeFilesHint: $('treeFilesHint'),
    treeFilesSortBy: $('treeFilesSortBy'),
    dupPaginationBar: $('dupPaginationBar'),
    folderDupPaginationBar: $('folderDupPaginationBar'),
    idlePaginationBar: $('idlePaginationBar'),
    idleSortBy: $('idleSortBy'),
    btnZoomIn: $('btnZoomIn'),
    btnZoomOut: $('btnZoomOut'),
    zoomLevelDisplay: $('zoomLevelDisplay'),
    privilegeBadge: $('privilegeBadge'),
    privilegeIcon: $('privilegeIcon'),
    privilegeText: $('privilegeText'),
    btnElevateAdmin: $('btnElevateAdmin'),
    liveBadge: $('liveBadge'),
    liveStatusText: $('liveStatusText'),

    drivesGrid: $('drivesGrid'),
    btnSelectAllDrives: $('btnSelectAllDrives'),
    btnRefreshDrives: $('btnRefreshDrives'),
    workerThreads: $('workerThreads'),
    workerThreadsHint: $('workerThreadsHint'),
    hashAlgo: $('hashAlgo'),
    hashMode: $('hashMode'),
    minFileSize: $('minFileSize'),
    autoSaveInterval: $('autoSaveInterval'),
    autoSaveBanner: $('autoSaveBanner'),
    autoSaveDetailsText: $('autoSaveDetailsText'),
    btnBannerRestore: $('btnBannerRestore'),
    btnBannerQuickScan: $('btnBannerQuickScan'),
    btnBannerDismiss: $('btnBannerDismiss'),
    btnStartScan: $('btnStartScan'),
    btnQuickScan: $('btnQuickScan'),
    btnCancelScan: $('btnCancelScan'),
    scanPhaseText: $('scanPhaseText'),
    progressHUD: $('progressHUD'),
    hudPhaseBadge: $('hudPhaseBadge'),
    hudQuickScanBadge: $('hudQuickScanBadge'),
    hudAutoSaveBadge: $('hudAutoSaveBadge'),
    hudErrorsBadge: $('hudErrorsBadge'),
    hudWorkersBadge: $('hudWorkersBadge'),
    statFiles: $('statFiles'),
    statDirs: $('statDirs'),
    statBytes: $('statBytes'),
    statAllocatedBytes: $('statAllocatedBytes'),
    boxReusedFiles: $('boxReusedFiles'),
    statReusedFiles: $('statReusedFiles'),
    boxNewModifiedFiles: $('boxNewModifiedFiles'),
    statNewModifiedFiles: $('statNewModifiedFiles'),
    statCompressedCount: $('statCompressedCount'),
    statCompressedSaved: $('statCompressedSaved'),
    statSkipped: $('statSkipped'),
    statPrehash: $('statPrehash'),
    statSpeed: $('statSpeed'),
    statElapsed: $('statElapsed'),
    progressBarFill: $('progressBarFill'),
    currentPathText: $('currentPathText'),
    progressPercentText: $('progressPercentText'),
    activeWorkersSection: $('activeWorkersSection'),
    activeWorkersGrid: $('activeWorkersGrid'),
    activeWorkerCountText: $('activeWorkerCountText'),
    recentFilesTableBody: $('recentFilesTableBody'),

    btnTreeGoUp: $('btnTreeGoUp'),
    treeBreadcrumbs: $('treeBreadcrumbs'),
    treeSplitLayout: $('treeSplitLayout'),
    treemapColorMode: $('treemapColorMode'),
    treemapDepth: $('treemapDepth'),
    treemapDepthVal: $('treemapDepthVal'),
    treeSearchInput: $('treeSearchInput'),
    btnTreeRefresh: $('btnTreeRefresh'),
    btnTreeMaxHeight: $('btnTreeMaxHeight'),
    treeSplitter: $('treeSplitter'),
    treeTableBody: $('treeTableBody'),
    treemapCurrentTitle: $('treemapCurrentTitle'),
    treemapCurrentSubtitle: $('treemapCurrentSubtitle'),
    btnResetZoom: $('btnResetZoom'),
    treemapContainer: $('treemapContainer'),
    treemapCanvas: $('treemapCanvas'),
    treemapTooltip: $('treemapTooltip'),
    treemapContextMenu: $('treemapContextMenu'),
    ctxZoomIn: $('ctxZoomIn'),
    ctxZoomOut: $('ctxZoomOut'),
    ctxCopyPath: $('ctxCopyPath'),
    ctxRecycle: $('ctxRecycle'),
    treemapLegendBar: $('treemapLegendBar'),

    dupCountBadge: $('dupCountBadge'),
    dupTotalGroups: $('dupTotalGroups'),
    dupTotalFiles: $('dupTotalFiles'),
    dupTotalWasted: $('dupTotalWasted'),
    dupSelectedCount: $('dupSelectedCount'),
    dupSortBy: $('dupSortBy'),
    dupMinSize: $('dupMinSize'),
    dupSearch: $('dupSearch'),
    btnSelectNewest: $('btnSelectNewest'),
    btnSelectOldest: $('btnSelectOldest'),
    btnClearSelection: $('btnClearSelection'),
    btnRecycleSelected: $('btnRecycleSelected'),
    btnDeleteSelectedDups: $('btnDeleteSelectedDups'),
    duplicatesContainer: $('duplicatesContainer'),

    folderDupCountBadge: $('folderDupCountBadge'),
    dupFolderTotalGroups: $('dupFolderTotalGroups'),
    dupFolderTotalCount: $('dupFolderTotalCount'),
    dupFolderTotalWasted: $('dupFolderTotalWasted'),
    dupFolderSortBy: $('dupFolderSortBy'),
    dupFolderMinSize: $('dupFolderMinSize'),
    dupFolderSearch: $('dupFolderSearch'),
    chkFolderTopLevelOnly: $('chkFolderTopLevelOnly'),
    btnRefreshFolderDuplicates: $('btnRefreshFolderDuplicates'),
    folderDuplicatesContainer: $('folderDuplicatesContainer'),
    comparePathA: $('comparePathA'),
    comparePathB: $('comparePathB'),
    btnSwapComparePaths: $('btnSwapComparePaths'),
    btnRunFolderCompare: $('btnRunFolderCompare'),
    folderCompareResults: $('folderCompareResults'),

    idleTotalCount: $('idleTotalCount'),
    idleTotalBytes: $('idleTotalBytes'),
    idleSelectedCount: $('idleSelectedCount'),
    idleBucketsGrid: $('idleBucketsGrid'),
    idleMinAge: $('idleMinAge'),
    idleMinSize: $('idleMinSize'),
    idleSearch: $('idleSearch'),
    btnRefreshIdle: $('btnRefreshIdle'),
    btnSelectAllIdle: $('btnSelectAllIdle'),
    btnClearIdleSelection: $('btnClearIdleSelection'),
    btnRecycleIdleSelected: $('btnRecycleIdleSelected'),
    btnDeleteIdleSelected: $('btnDeleteIdleSelected'),
    idleTableBody: $('idleTableBody'),
    idleSelectAllCheckbox: $('idleSelectAllCheckbox'),

    btnOpenSaveCacheModal: $('btnOpenSaveCacheModal'),
    btnOpenLoadCacheModal: $('btnOpenLoadCacheModal'),
    saveCacheModal: $('saveCacheModal'),
    loadCacheModal: $('loadCacheModal'),
    saveCacheFileName: $('saveCacheFileName'),
    btnConfirmSaveCache: $('btnConfirmSaveCache'),
    savedCachesList: $('savedCachesList'),
    customCachePath: $('customCachePath'),
    btnLoadCustomCache: $('btnLoadCustomCache'),

    extensionsTableBody: $('extensionsTableBody'),
    eventCountBadge: $('eventCountBadge'),
    watcherStateTitle: $('watcherStateTitle'),
    watcherStateDesc: $('watcherStateDesc'),
    eventLogsTableBody: $('eventLogsTableBody'),
    btnClearLogs: $('btnClearLogs'),
    btnRefreshEventLogs: $('btnRefreshEventLogs'),
    skippedItemsTableBody: $('skippedItemsTableBody'),
    skippedCountBadge: $('skippedCountBadge'),
    btnRefreshSkipped: $('btnRefreshSkipped'),
    toastContainer: $('toastContainer'),

    aiModelSelect: $('aiModelSelect'),
    btnPullSelectedModel: $('btnPullSelectedModel'),
    btnOpenAIConfigModal: $('btnOpenAIConfigModal'),
    aiConfigModal: $('aiConfigModal'),
    aiOllamaEndpointInput: $('aiOllamaEndpointInput'),
    aiOpenRouterKeyInput: $('aiOpenRouterKeyInput'),
    aiOpenRouterKeyHint: $('aiOpenRouterKeyHint'),
    btnClearOpenRouterKey: $('btnClearOpenRouterKey'),
    aiDryRunDefaultCheckbox: $('aiDryRunDefaultCheckbox'),
    btnSaveAIConfig: $('btnSaveAIConfig'),
    aiPullProgressContainer: $('aiPullProgressContainer'),
    pullModelTitle: $('pullModelTitle'),
    pullModelPercent: $('pullModelPercent'),
    pullProgressBar: $('pullProgressBar'),
    pullModelStatus: $('pullModelStatus'),
    aiMessagesContainer: $('aiMessagesContainer'),
    aiPromptInput: $('aiPromptInput'),
    btnSendAIMessage: $('btnSendAIMessage'),
    btnClearAIChat: $('btnClearAIChat'),
    aiModelsList: $('aiModelsList'),
    ollamaStatusDot: $('ollamaStatusDot'),
    openrouterStatusDot: $('openrouterStatusDot'),
    quickStatusDot: $('quickStatusDot'),

    globalProgressOverlay: $('globalProgressOverlay'),
    globalProgressTitle: $('globalProgressTitle'),
    globalProgressDesc: $('globalProgressDesc'),
    globalProgressBar: $('globalProgressBar'),
    globalProgressPercent: $('globalProgressPercent'),
    globalProgressDetail: $('globalProgressDetail'),

    confirmActionModal: $('confirmActionModal'),
    confirmActionTitle: $('confirmActionTitle'),
    confirmActionDesc: $('confirmActionDesc'),
    confirmActionCount: $('confirmActionCount'),
    confirmActionSize: $('confirmActionSize'),
    confirmActionFilesBox: $('confirmActionFilesBox'),
    confirmActionFiles: $('confirmActionFiles'),
    confirmActionList: $('confirmActionList'),
    confirmActionInputGroup: $('confirmActionInputGroup'),
    confirmActionInputLabel: $('confirmActionInputLabel'),
    confirmActionInput: $('confirmActionInput'),
    confirmActionInputHint: $('confirmActionInputHint'),
    confirmActionError: $('confirmActionError'),
    btnConfirmAction: $('btnConfirmAction'),

    actionResultsModal: $('actionResultsModal'),
    actionResultsTitle: $('actionResultsTitle'),
    actionResultsSummary: $('actionResultsSummary'),
    actionResultsTableBody: $('actionResultsTableBody'),
  };

  // ==========================================
  // UTILIDADES DE INTERFACE
  // ==========================================

  function debounce(func, wait) {
    var timeout;
    return function () {
      var args = arguments;
      var self = this;
      clearTimeout(timeout);
      timeout = setTimeout(function () { func.apply(self, args); }, wait);
    };
  }

  /** Toast sem innerHTML: a mensagem entra como texto puro. */
  function showToast(message, type) {
    if (!elements.toastContainer) return;
    var toast = document.createElement('div');
    toast.className = 'toast ' + (type || 'info');
    var span = document.createElement('span');
    span.textContent = String(message == null ? '' : message);
    toast.appendChild(span);
    elements.toastContainer.appendChild(toast);
    setTimeout(function () {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      setTimeout(function () { toast.remove(); }, 300);
    }, 4000);
  }

  function setGlobalOperationLock(isLocked, title, desc, percent, detail) {
    state.isBusyOperation = isLocked;

    var actionButtons = [
      elements.btnStartScan,
      elements.btnQuickScan,
      elements.btnOpenSaveCacheModal,
      elements.btnOpenLoadCacheModal,
      elements.btnBannerRestore,
      elements.btnBannerQuickScan,
      elements.btnBannerDismiss,
      elements.btnRefreshDrives,
      elements.btnSelectAllDrives,
      elements.btnConfirmSaveCache,
      elements.btnLoadCustomCache,
    ];

    actionButtons.forEach(function (btn) {
      if (!btn) return;
      btn.disabled = isLocked;
      btn.classList.toggle('disabled-action', !!isLocked);
    });

    if (!elements.globalProgressOverlay) return;
    if (isLocked) {
      elements.globalProgressOverlay.classList.remove('hidden');
      if (title && elements.globalProgressTitle) elements.globalProgressTitle.textContent = title;
      if (desc && elements.globalProgressDesc) elements.globalProgressDesc.textContent = desc;
      updateGlobalProgress(percent || 0, detail || '');
    } else {
      elements.globalProgressOverlay.classList.add('hidden');
    }
  }

  function updateGlobalProgress(percent, detail) {
    var clamped = Math.max(0, Math.min(100, Math.round(percent || 0)));
    if (elements.globalProgressBar) elements.globalProgressBar.style.width = clamped + '%';
    if (elements.globalProgressPercent) elements.globalProgressPercent.textContent = clamped + '%';
    if (detail && elements.globalProgressDetail) elements.globalProgressDetail.textContent = detail;
  }

  function openModal(modal) {
    if (modal) modal.classList.remove('hidden');
  }

  function closeModal(modal) {
    if (modal) modal.classList.add('hidden');
  }

  // ==========================================
  // PAGINADOR
  // ==========================================

  function renderPaginationBar(container, totalItems, currentPage, pageSize, onPageChange, onPageSizeChange, unitLabel) {
    if (!container) return;
    if (!totalItems || totalItems <= 0) {
      container.textContent = '';
      return;
    }

    var bounds = C.pageBounds(totalItems, currentPage, pageSize);
    var totalPages = bounds.totalPages;
    var page = bounds.page;

    var pagesHtml = '';
    var maxButtons = 5;
    var startPage = Math.max(1, page - Math.floor(maxButtons / 2));
    var endPage = Math.min(totalPages, startPage + maxButtons - 1);
    if (endPage - startPage + 1 < maxButtons) startPage = Math.max(1, endPage - maxButtons + 1);

    if (startPage > 1) {
      pagesHtml += '<button class="pagination-page-btn" data-page="1">1</button>';
      if (startPage > 2) pagesHtml += '<span class="pagination-ellipsis">...</span>';
    }
    for (var p = startPage; p <= endPage; p++) {
      pagesHtml += '<button class="pagination-page-btn ' + (p === page ? 'active' : '') + '" data-page="' + p + '">' + p + '</button>';
    }
    if (endPage < totalPages) {
      if (endPage < totalPages - 1) pagesHtml += '<span class="pagination-ellipsis">...</span>';
      pagesHtml += '<button class="pagination-page-btn" data-page="' + totalPages + '">' + totalPages + '</button>';
    }

    // Todos os valores interpolados aqui são números inteiros ou o rótulo fixo.
    container.innerHTML =
      '<div class="pagination-left">' +
        '<span class="pagination-info">Exibindo <strong>' + esc(formatNumber(bounds.firstItem)) + '</strong> - <strong>' +
        esc(formatNumber(bounds.lastItem)) + '</strong> de <strong>' + esc(formatNumber(totalItems)) + '</strong> ' +
        esc(unitLabel || 'itens') + '</span>' +
      '</div>' +
      '<div class="pagination-right">' +
        '<div class="pagination-pages">' +
          '<button class="pagination-btn" id="pgBtnFirst" ' + (page <= 1 ? 'disabled' : '') + ' title="Primeira Página">&laquo;</button>' +
          '<button class="pagination-btn" id="pgBtnPrev" ' + (page <= 1 ? 'disabled' : '') + ' title="Página Anterior">&lsaquo;</button>' +
          pagesHtml +
          '<button class="pagination-btn" id="pgBtnNext" ' + (page >= totalPages ? 'disabled' : '') + ' title="Próxima Página">&rsaquo;</button>' +
          '<button class="pagination-btn" id="pgBtnLast" ' + (page >= totalPages ? 'disabled' : '') + ' title="Última Página">&raquo;</button>' +
        '</div>' +
        (onPageSizeChange ? (
          '<select class="pagination-select" id="pgSelectLimit" title="Itens por página">' +
            [25, 50, 100, 250, 500].map(function (n) {
              return '<option value="' + n + '" ' + (pageSize === n ? 'selected' : '') + '>' + n + ' / pág</option>';
            }).join('') +
          '</select>'
        ) : '') +
      '</div>';

    container.querySelectorAll('.pagination-page-btn').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var target = parseInt(btn.getAttribute('data-page'), 10);
        if (target && target !== page) onPageChange(target);
      });
    });

    var first = container.querySelector('#pgBtnFirst');
    if (first) first.addEventListener('click', function () { onPageChange(1); });
    var prev = container.querySelector('#pgBtnPrev');
    if (prev) prev.addEventListener('click', function () { onPageChange(page - 1); });
    var next = container.querySelector('#pgBtnNext');
    if (next) next.addEventListener('click', function () { onPageChange(page + 1); });
    var last = container.querySelector('#pgBtnLast');
    if (last) last.addEventListener('click', function () { onPageChange(totalPages); });

    var select = container.querySelector('#pgSelectLimit');
    if (select && onPageSizeChange) {
      select.addEventListener('change', function (e) {
        var newLimit = parseInt(e.target.value, 10);
        if (newLimit) onPageSizeChange(newLimit);
      });
    }
  }

  // ==========================================
  // TEMA (12 temas)
  // ==========================================

  function setupThemeManager() {
    // localStorage aqui é só cache de pintura inicial; a fonte da verdade é a
    // Configuração no servidor, aplicada assim que o GET /api/config responde.
    var cached = 'theme-ochre-dark';
    try { cached = localStorage.getItem('scanfile_theme') || cached; } catch (e) { /* modo privado */ }
    applyTheme(cached, false);

    if (elements.themeSelector) {
      elements.themeSelector.value = state.theme;
      elements.themeSelector.addEventListener('change', function (e) {
        applyTheme(e.target.value, true);
      });
    }
  }

  function normalizeTheme(theme) {
    if (!theme) return 'theme-ochre-dark';
    if (theme === 'ochre-dark') return 'theme-ochre-dark';
    if (theme === 'ochre-light') return 'theme-ochre-sand';
    if (theme === 'obsidian') return 'theme-dark-obsidian';
    if (String(theme).indexOf('theme-') !== 0) return 'theme-' + theme;
    return theme;
  }

  function applyTheme(theme, save) {
    var normalized = normalizeTheme(theme);

    var toRemove = [];
    document.body.classList.forEach(function (cls) {
      if (cls.indexOf('theme-') === 0 || cls === 'dark-theme' || cls === 'light-theme') toRemove.push(cls);
    });
    toRemove.forEach(function (cls) { document.body.classList.remove(cls); });

    document.body.classList.add(normalized);
    state.theme = normalized;
    try { localStorage.setItem('scanfile_theme', normalized); } catch (e) { /* modo privado */ }

    if (elements.themeSelector && elements.themeSelector.value !== normalized) {
      elements.themeSelector.value = normalized;
    }

    if (state.currentTab === 'treeTab') {
      setTimeout(resizeTreemapCanvas, 50);
    }

    if (save) queueConfigChange({ theme: normalized });
  }

  // ==========================================
  // CONFIGURAÇÃO: carrega uma vez e salva só o que mudou (1.6, C3, M13)
  // ==========================================

  async function loadSavedConfig() {
    var cfg = null;
    try {
      cfg = await apiGetJSON('/api/config');
    } catch (err) {
      console.warn('Não foi possível carregar a Configuração:', err);
    }

    if (!cfg || typeof cfg !== 'object') {
      // Falha na leitura: mantemos os controles como estão e NUNCA gravamos
      // valores padrão por cima do arquivo do usuário (M13). Os salvamentos
      // seguintes continuam válidos porque enviam apenas as chaves alteradas.
      state.configLoaded = false;
      state.serverConfig = null;
      showToast('Não foi possível ler as preferências salvas. Nada será sobrescrito.', 'warning');
      return;
    }

    state.serverConfig = cfg;
    state.configLoaded = true;
    state.hasOpenRouterKey = !!cfg.hasOpenRouterKey;

    if (cfg.theme) applyTheme(cfg.theme, false);
    if (cfg.uiZoom) setUIZoom(cfg.uiZoom, false);

    setSelectValue(elements.hashAlgo, cfg.hashAlgorithm);
    setSelectValue(elements.hashMode, cfg.hashMode);
    if (cfg.minFileSize !== undefined) setSelectValue(elements.minFileSize, String(cfg.minFileSize));
    if (cfg.autoSaveIntervalMinutes !== undefined) setSelectValue(elements.autoSaveInterval, String(cfg.autoSaveIntervalMinutes));

    if (cfg.treemapDepth && elements.treemapDepth) {
      elements.treemapDepth.value = String(cfg.treemapDepth);
      state.treemap.depth = cfg.treemapDepth;
      if (elements.treemapDepthVal) elements.treemapDepthVal.textContent = cfg.treemapDepth + ' níveis';
    }
    if (cfg.treemapColorMode) {
      setSelectValue(elements.treemapColorMode, cfg.treemapColorMode);
      state.treemap.colorMode = cfg.treemapColorMode;
    }
    if (cfg.treemapViewMode) {
      state.treemap.viewMode = cfg.treemapViewMode;
      document.querySelectorAll('[data-tree-view]').forEach(function (b) {
        b.classList.toggle('active', b.getAttribute('data-tree-view') === cfg.treemapViewMode);
      });
      if (elements.treeSplitLayout) {
        elements.treeSplitLayout.className = 'tree-split-layout view-' + cfg.treemapViewMode;
      }
    }
    // A tabela de arquivos usa 100 por página, como manda o contrato 1.4;
    // `treeTableLimit` da Configuração não sobrepõe esse tamanho.

    setSelectValue(elements.dupSortBy, cfg.duplicatesSortBy);
    if (cfg.duplicatesMinSize !== undefined) setSelectValue(elements.dupMinSize, String(cfg.duplicatesMinSize));
    if (cfg.dupLimit) state.dupLimit = cfg.dupLimit;

    if (cfg.idleMinAgeDays) setSelectValue(elements.idleMinAge, String(cfg.idleMinAgeDays));
    if (cfg.idleMinSizeBytes !== undefined) setSelectValue(elements.idleMinSize, String(cfg.idleMinSizeBytes));
    setSelectValue(elements.idleSortBy, cfg.idleSortBy);
    if (cfg.idleLimit) state.idleLimit = cfg.idleLimit;

    setSelectValue(elements.dupFolderSortBy, cfg.folderSortBy);
    if (cfg.folderMinSize !== undefined) setSelectValue(elements.dupFolderMinSize, String(cfg.folderMinSize));
    if (cfg.folderDupLimit) state.folderDupLimit = cfg.folderDupLimit;
    if (cfg.chkFolderTopLevelOnly !== undefined && elements.chkFolderTopLevelOnly) {
      elements.chkFolderTopLevelOnly.checked = !!cfg.chkFolderTopLevelOnly;
    }

    if (cfg.comparePathA && elements.comparePathA) elements.comparePathA.value = cfg.comparePathA;
    if (cfg.comparePathB && elements.comparePathB) elements.comparePathB.value = cfg.comparePathB;

    if (Array.isArray(cfg.selectedRoots) && cfg.selectedRoots.length > 0) {
      state.selectedRoots = new Set(cfg.selectedRoots);
    }

    state.ai.provider = C.normalizeProvider(cfg.aiProvider);
    markActiveProviderButton();
  }

  /** Só troca o valor do select se a opção existir, para não zerar o controle. */
  function setSelectValue(select, value) {
    if (!select || value === undefined || value === null || value === '') return;
    var wanted = String(value);
    for (var i = 0; i < select.options.length; i++) {
      if (select.options[i].value === wanted) {
        select.value = wanted;
        return;
      }
    }
  }

  var flushConfigPatch = debounce(async function () {
    var patch = state.pendingConfigPatch;
    state.pendingConfigPatch = {};
    if (!C.hasChanges(patch)) return;

    // Compara com o que o servidor já tem: nada redundante sobe.
    var effective = state.configLoaded ? C.diffConfig(state.serverConfig, patch) : patch;
    if (!C.hasChanges(effective)) return;

    try {
      var res = await apiPostJSON('/api/config', effective);
      if (!res.ok) {
        console.warn('Falha ao salvar preferências:', res.error);
        return;
      }
      if (state.serverConfig) Object.assign(state.serverConfig, effective);
      if (Object.prototype.hasOwnProperty.call(effective, 'aiOpenRouterKey')) {
        state.hasOpenRouterKey = !!effective.aiOpenRouterKey;
        if (state.serverConfig) state.serverConfig.aiOpenRouterKey = '';
        updateOpenRouterKeyHint();
      }
    } catch (err) {
      console.warn('Falha ao salvar preferências:', err);
    }
  }, 300);

  /**
   * Registra uma mudança de preferência. Só as chaves passadas aqui são
   * enviadas — nunca o objeto inteiro (C3).
   */
  function queueConfigChange(patch) {
    if (!patch) return;
    Object.assign(state.pendingConfigPatch, patch);
    flushConfigPatch();
  }

  function currentSelectedRootsArray() {
    return Array.from(state.selectedRoots);
  }

  // ==========================================
  // INICIALIZAÇÃO
  // ==========================================

  async function init() {
    window.addEventListener('error', function (e) {
      console.warn('[ScanFile] Exceção capturada:', e.error || e.message);
    });
    window.addEventListener('unhandledrejection', function (e) {
      console.warn('[ScanFile] Rejeição assíncrona capturada:', e.reason);
    });

    sessionToken = readSessionToken();

    safely('setupThemeManager', setupThemeManager);
    safely('setupTabs', setupTabs);
    safely('setupEventListeners', setupEventListeners);
    safely('setupConfirmModals', setupConfirmModals);
    safely('setupTreemap', setupTreemap);
    safely('setupCacheModals', setupCacheModals);
    safely('setupFolderComparator', setupFolderComparator);
    safely('setupAIAssistant', setupAIAssistant);
    safely('setupLifecycle', setupLifecycle);

    fetchPrivileges();
    await safelyAsync('loadSavedConfig', loadSavedConfig);
    await safelyAsync('fetchSystemInfo', fetchSystemInfo);
    fetchDrives();
    setupSSE();
    fetchScanStatus();
    fetchEventLogs();
    fetchSkippedItems();
    checkAutoSaveStatus();
    pollMemoryStats();
    // O SSE já traz memoryStats durante a Varredura; esta sondagem lenta só
    // cobre o período ocioso e desiste se o SSE acabou de atualizar (M11).
    setInterval(pollMemoryStats, 10000);
  }

  function safely(name, fn) {
    try { fn(); } catch (e) { console.error('Erro em ' + name + ':', e); }
  }

  async function safelyAsync(name, fn) {
    try { await fn(); } catch (e) { console.error('Erro em ' + name + ':', e); }
  }

  /** Presença: avisa o servidor quando a Janela fecha (contrato 1.9). */
  function setupLifecycle() {
    window.addEventListener('pagehide', function () {
      try {
        if (navigator.sendBeacon) {
          navigator.sendBeacon(tokenQuery('/api/ui/closed'));
        }
      } catch (e) { /* a janela já está indo embora */ }
    });
  }

  // ==========================================
  // ABAS
  // ==========================================

  function setupTabs() {
    document.querySelectorAll('.nav-tab').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var targetTab = btn.getAttribute('data-tab');
        var targetPane = document.getElementById(targetTab);
        if (!targetPane) return;

        document.querySelectorAll('.nav-tab').forEach(function (b) { b.classList.remove('active'); });
        document.querySelectorAll('.tab-pane').forEach(function (p) { p.classList.remove('active'); });

        btn.classList.add('active');
        targetPane.classList.add('active');
        state.currentTab = targetTab;

        if (targetTab === 'treeTab') {
          loadTreeData(state.treePath);
          setTimeout(resizeTreemapCanvas, 50);
        } else if (targetTab === 'duplicatesTab') {
          loadDuplicates(state.dupPage);
        } else if (targetTab === 'foldersTab') {
          loadFolderDuplicates(state.folderDupPage);
        } else if (targetTab === 'analyticsTab') {
          loadAnalytics();
        } else if (targetTab === 'idleTab') {
          loadIdleFiles(state.idlePage);
        } else if (targetTab === 'watcherTab') {
          fetchEventLogs();
          fetchSkippedItems();
        } else if (targetTab === 'aiTab') {
          loadAICatalog();
        }
      });
    });
  }

  // ==========================================
  // LISTENERS GERAIS
  // ==========================================

  function setupEventListeners() {
    if (elements.btnElevateAdmin) elements.btnElevateAdmin.addEventListener('click', elevateToAdmin);
    if (elements.btnRefreshDrives) elements.btnRefreshDrives.addEventListener('click', fetchDrives);

    if (elements.btnSelectAllDrives) {
      elements.btnSelectAllDrives.addEventListener('click', function () {
        if (!state.drives) return;
        var allSelected = state.selectedRoots.size === state.drives.length;
        state.selectedRoots.clear();
        if (!allSelected) {
          state.drives.forEach(function (d) { state.selectedRoots.add(d.letter); });
        }
        renderDrivesGrid();
        queueConfigChange({ selectedRoots: currentSelectedRootsArray() });
      });
    }

    if (elements.btnStartScan) elements.btnStartScan.addEventListener('click', function () { startScan(false); });
    if (elements.btnQuickScan) elements.btnQuickScan.addEventListener('click', function () { startScan(true); });
    if (elements.btnCancelScan) elements.btnCancelScan.addEventListener('click', cancelScan);

    if (elements.btnBannerRestore) elements.btnBannerRestore.addEventListener('click', function () { restoreAutoSave(); });
    if (elements.btnBannerQuickScan) {
      elements.btnBannerQuickScan.addEventListener('click', async function () {
        try {
          await restoreAutoSave();
          startScan(true);
        } catch (e) { /* restoreAutoSave já avisou */ }
      });
    }
    if (elements.btnBannerDismiss) {
      elements.btnBannerDismiss.addEventListener('click', function () {
        if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
      });
    }

    if (elements.workerThreads) {
      elements.workerThreads.addEventListener('change', function () {
        queueConfigChange({ workerThreads: parseInt(elements.workerThreads.value, 10) || 0 });
      });
    }
    if (elements.hashAlgo) {
      elements.hashAlgo.addEventListener('change', function () {
        queueConfigChange({ hashAlgorithm: elements.hashAlgo.value });
      });
    }
    if (elements.hashMode) {
      elements.hashMode.addEventListener('change', function () {
        queueConfigChange({ hashMode: elements.hashMode.value });
      });
    }
    if (elements.minFileSize) {
      elements.minFileSize.addEventListener('change', function () {
        queueConfigChange({ minFileSize: parseInt(elements.minFileSize.value, 10) || 1 });
      });
    }
    if (elements.autoSaveInterval) {
      elements.autoSaveInterval.addEventListener('change', function () {
        queueConfigChange({ autoSaveIntervalMinutes: parseInt(elements.autoSaveInterval.value, 10) || 0 });
      });
    }

    if (elements.btnTreeRefresh) elements.btnTreeRefresh.addEventListener('click', function () { loadTreeData(state.treePath); });
    if (elements.treeSearchInput) elements.treeSearchInput.addEventListener('input', debounce(renderTreeTable, 250));
    if (elements.btnTreeGoUp) elements.btnTreeGoUp.addEventListener('click', treeGoUp);
    if (elements.btnResetZoom) elements.btnResetZoom.addEventListener('click', function () { loadTreeData(''); });

    if (elements.treeFilesSortBy) {
      elements.treeFilesSortBy.addEventListener('change', function () {
        state.treeFilesSortBy = elements.treeFilesSortBy.value;
        loadTreeFilesPage(1);
      });
    }

    document.querySelectorAll('[data-tree-view]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var view = btn.getAttribute('data-tree-view');
        document.querySelectorAll('[data-tree-view]').forEach(function (b) { b.classList.remove('active'); });
        btn.classList.add('active');
        state.treemap.viewMode = view;
        if (elements.treeSplitLayout) {
          elements.treeSplitLayout.className = 'tree-split-layout view-' + view;
        }
        setTimeout(resizeTreemapCanvas, 80);
        queueConfigChange({ treemapViewMode: view });
      });
    });

    if (elements.treemapColorMode) {
      elements.treemapColorMode.addEventListener('change', function (e) {
        state.treemap.colorMode = e.target.value;
        renderTreemapCanvas();
        renderTreemapLegend();
        queueConfigChange({ treemapColorMode: e.target.value });
      });
    }

    if (elements.treemapDepth) {
      elements.treemapDepth.addEventListener('input', function (e) {
        var val = parseInt(e.target.value, 10);
        state.treemap.depth = val;
        if (elements.treemapDepthVal) elements.treemapDepthVal.textContent = val + ' níveis';
      });
      elements.treemapDepth.addEventListener('change', function () {
        loadTreeData(state.treePath);
        queueConfigChange({ treemapDepth: state.treemap.depth });
      });
    }

    if (elements.dupSortBy) {
      elements.dupSortBy.addEventListener('change', function () {
        loadDuplicates(1);
        queueConfigChange({ duplicatesSortBy: elements.dupSortBy.value });
      });
    }
    if (elements.dupMinSize) {
      elements.dupMinSize.addEventListener('change', function () {
        loadDuplicates(1);
        queueConfigChange({ duplicatesMinSize: parseInt(elements.dupMinSize.value, 10) || 0 });
      });
    }
    if (elements.dupSearch) elements.dupSearch.addEventListener('input', debounce(function () { loadDuplicates(1); }, 300));

    if (elements.btnSelectNewest) elements.btnSelectNewest.addEventListener('click', function () { selectDuplicatesByStrategy('keep_newest'); });
    if (elements.btnSelectOldest) elements.btnSelectOldest.addEventListener('click', function () { selectDuplicatesByStrategy('keep_oldest'); });
    if (elements.btnClearSelection) elements.btnClearSelection.addEventListener('click', clearSelection);
    if (elements.btnRecycleSelected) elements.btnRecycleSelected.addEventListener('click', function () { requestRecycleFiles('duplicates'); });
    if (elements.btnDeleteSelectedDups) elements.btnDeleteSelectedDups.addEventListener('click', function () { requestDeleteFiles('duplicates'); });

    if (elements.idleMinAge) {
      elements.idleMinAge.addEventListener('change', function () {
        loadIdleFiles(1);
        queueConfigChange({ idleMinAgeDays: parseInt(elements.idleMinAge.value, 10) || 365 });
      });
    }
    if (elements.idleMinSize) {
      elements.idleMinSize.addEventListener('change', function () {
        loadIdleFiles(1);
        queueConfigChange({ idleMinSizeBytes: parseInt(elements.idleMinSize.value, 10) || 0 });
      });
    }
    if (elements.idleSortBy) {
      elements.idleSortBy.addEventListener('change', function () {
        loadIdleFiles(1);
        queueConfigChange({ idleSortBy: elements.idleSortBy.value });
      });
    }
    if (elements.idleSearch) elements.idleSearch.addEventListener('input', debounce(function () { loadIdleFiles(1); }, 300));
    if (elements.btnRefreshIdle) elements.btnRefreshIdle.addEventListener('click', function () { loadIdleFiles(1); });
    if (elements.btnSelectAllIdle) elements.btnSelectAllIdle.addEventListener('click', selectAllIdleFiles);
    if (elements.btnClearIdleSelection) elements.btnClearIdleSelection.addEventListener('click', clearIdleSelection);
    if (elements.btnRecycleIdleSelected) elements.btnRecycleIdleSelected.addEventListener('click', function () { requestRecycleFiles('idle'); });
    if (elements.btnDeleteIdleSelected) elements.btnDeleteIdleSelected.addEventListener('click', function () { requestDeleteFiles('idle'); });
    if (elements.idleSelectAllCheckbox) {
      elements.idleSelectAllCheckbox.addEventListener('change', function (e) {
        if (e.target.checked) selectAllIdleFiles();
        else clearIdleSelection();
      });
    }

    // Filtros de pastas: registrados uma única vez (antes havia listener duplo).
    if (elements.dupFolderSortBy) {
      elements.dupFolderSortBy.addEventListener('change', function () {
        loadFolderDuplicates(1);
        queueConfigChange({ folderSortBy: elements.dupFolderSortBy.value });
      });
    }
    if (elements.dupFolderMinSize) {
      elements.dupFolderMinSize.addEventListener('change', function () {
        loadFolderDuplicates(1);
        queueConfigChange({ folderMinSize: parseInt(elements.dupFolderMinSize.value, 10) || 0 });
      });
    }
    if (elements.dupFolderSearch) elements.dupFolderSearch.addEventListener('input', debounce(function () { loadFolderDuplicates(1); }, 300));
    if (elements.chkFolderTopLevelOnly) {
      elements.chkFolderTopLevelOnly.addEventListener('change', function () {
        loadFolderDuplicates(1);
        queueConfigChange({ chkFolderTopLevelOnly: elements.chkFolderTopLevelOnly.checked });
      });
    }
    if (elements.btnRefreshFolderDuplicates) elements.btnRefreshFolderDuplicates.addEventListener('click', function () { loadFolderDuplicates(1); });
    if (elements.comparePathA) elements.comparePathA.addEventListener('change', function () { queueConfigChange({ comparePathA: elements.comparePathA.value.trim() }); });
    if (elements.comparePathB) elements.comparePathB.addEventListener('change', function () { queueConfigChange({ comparePathB: elements.comparePathB.value.trim() }); });

    if (elements.btnZoomIn) elements.btnZoomIn.addEventListener('click', function () { setUIZoom(state.uiZoom + 5); });
    if (elements.btnZoomOut) elements.btnZoomOut.addEventListener('click', function () { setUIZoom(state.uiZoom - 5); });
    if (elements.zoomLevelDisplay) elements.zoomLevelDisplay.addEventListener('click', function () { setUIZoom(100); });

    window.addEventListener('keydown', function (e) {
      if (!(e.ctrlKey || e.metaKey)) return;
      if (e.key === '=' || e.key === '+') {
        e.preventDefault();
        setUIZoom(state.uiZoom + 5);
      } else if (e.key === '-' || e.key === '_') {
        e.preventDefault();
        setUIZoom(state.uiZoom - 5);
      } else if (e.key === '0') {
        e.preventDefault();
        setUIZoom(100);
      }
    });

    if (elements.btnClearLogs) {
      elements.btnClearLogs.addEventListener('click', function () {
        state.eventLogs = [];
        renderEventLogs();
      });
    }
    if (elements.btnRefreshEventLogs) elements.btnRefreshEventLogs.addEventListener('click', fetchEventLogs);
    if (elements.btnRefreshSkipped) elements.btnRefreshSkipped.addEventListener('click', fetchSkippedItems);
  }

  function setUIZoom(percent, save) {
    var clamped = Math.max(60, Math.min(180, Math.round(percent || 100)));
    state.uiZoom = clamped;
    document.body.style.zoom = clamped + '%';

    if (elements.zoomLevelDisplay) elements.zoomLevelDisplay.textContent = clamped + '%';

    // O treemap é remedido em px de layout; o clique é convertido depois pela
    // razão entre o retângulo visual e essas medidas (core.mapClientToCanvas).
    setTimeout(resizeTreemapCanvas, 60);

    if (save !== false) queueConfigChange({ uiZoom: clamped });
  }

  // ==========================================
  // SISTEMA: informações, threads e privilégios
  // ==========================================

  /** Alimenta o combo de threads com /api/system/info (contrato 1.3). */
  async function fetchSystemInfo() {
    var info = null;
    try {
      info = await apiGetJSON('/api/system/info');
    } catch (err) {
      console.warn('Não foi possível consultar /api/system/info:', err);
    }

    if (!info || typeof info !== 'object') {
      // Reserva local: o número de núcleos que o navegador conhece.
      info = { numCPU: navigator.hardwareConcurrency || 8 };
    }
    state.systemInfo = info;

    var numCPU = Number(info.numCPU) || navigator.hardwareConcurrency || 8;
    var options = Array.isArray(info.threadOptions) && info.threadOptions.length > 0
      ? info.threadOptions
      : C.buildThreadOptions(numCPU);

    renderThreadOptions(options, numCPU);

    if (elements.workerThreadsHint) {
      var maxThreads = Number(info.maxThreads) || options[options.length - 1];
      elements.workerThreadsHint.textContent =
        numCPU + ' núcleos detectados. O servidor sempre aplica no máximo ' + maxThreads + ' threads.';
    }
  }

  function renderThreadOptions(options, numCPU) {
    var select = elements.workerThreads;
    if (!select) return;

    var desired = state.serverConfig && state.serverConfig.workerThreads !== undefined
      ? String(state.serverConfig.workerThreads)
      : select.value;

    select.textContent = '';
    options.forEach(function (value) {
      var opt = document.createElement('option');
      opt.value = String(value);
      opt.textContent = C.threadOptionLabel(value, numCPU);
      select.appendChild(opt);
    });

    setSelectValue(select, desired);
    if (!select.value) select.value = '0';
  }

  async function fetchPrivileges() {
    var data = null;
    try {
      data = await apiGetJSON('/api/system/privileges');
    } catch (err) {
      console.warn('Erro ao consultar privilégios:', err);
      return;
    }
    if (!data || !elements.privilegeBadge) return;

    if (data.isElevated) {
      elements.privilegeBadge.className = 'privilege-badge admin';
      elements.privilegeIcon.textContent = '👑';
      elements.privilegeText.textContent = data.hasBackupAccess
        ? 'Administrador (SeBackupPrivilege)'
        : 'Administrador (Elevado)';
      elements.privilegeBadge.title = 'Executando como Administrador (' + (data.activeUser || '') +
        '). SeBackupPrivilege ativo: leitura de qualquer pasta do NTFS.';
      if (elements.btnElevateAdmin) elements.btnElevateAdmin.classList.add('hidden');
    } else {
      elements.privilegeBadge.className = 'privilege-badge standard';
      elements.privilegeIcon.textContent = '🛡️';
      elements.privilegeText.textContent = 'Usuário Padrão';
      elements.privilegeBadge.title = 'Modo padrão. Clique em Elevar para abrir como Administrador.';
      if (elements.btnElevateAdmin) elements.btnElevateAdmin.classList.remove('hidden');
    }
  }

  async function elevateToAdmin() {
    showToast('Solicitando elevação de privilégios ao Windows...', 'info');
    var res = await apiPostJSON('/api/system/elevate', {});
    if (!res.ok) {
      showToast('Falha na elevação: ' + res.error, 'danger');
      return;
    }
    showToast('Elevação solicitada. Confirme o prompt do Windows.', 'success');
  }

  // ==========================================
  // TELEMETRIA DE MEMÓRIA
  // ==========================================

  async function pollMemoryStats() {
    // Durante a Varredura o SSE entrega memoryStats a cada evento de progresso.
    if (Date.now() - state.lastProgressAt < 5000) return;
    try {
      var data = await apiGetJSON('/api/system/memory');
      updateMemoryTelemetry(data);
    } catch (e) { /* sondagem silenciosa */ }
  }

  function updateMemoryTelemetry(mem) {
    if (!mem) return;
    var allocMB = mem.allocMB !== undefined ? mem.allocMB : 0;
    var sysMB = mem.sysMB !== undefined ? mem.sysMB : 0;
    var sysTotalGB = ((mem.systemTotalRAMMB || 0) / 1024).toFixed(1);
    var sysUsedGB = ((mem.systemUsedRAMMB || 0) / 1024).toFixed(1);
    var sysPct = Math.round(mem.systemPercent || 0);

    if (elements.memAppUsage) elements.memAppUsage.textContent = allocMB + ' MB (Heap: ' + sysMB + ' MB)';
    if (elements.memSysUsage) elements.memSysUsage.textContent = 'SO: ' + sysUsedGB + ' / ' + sysTotalGB + ' GB (' + sysPct + '%)';
    if (elements.memoryBarFill) {
      elements.memoryBarFill.style.width = Math.min(100, Math.max(2, sysPct)) + '%';
      elements.memoryBarFill.style.background = sysPct > 85
        ? 'linear-gradient(90deg, #f59e0b, #ef4444)'
        : 'linear-gradient(90deg, #10b981, #f59e0b)';
    }
  }

  // ==========================================
  // DISCOS (contrato 1.12)
  // ==========================================

  async function fetchDrives() {
    if (!elements.drivesGrid) return;
    elements.drivesGrid.textContent = 'Detectando unidades de disco do sistema...';
    elements.drivesGrid.className = 'drives-grid';

    var controller = new AbortController();
    var timeoutId = setTimeout(function () { controller.abort(); }, 8000);

    try {
      var data = await apiGetJSON('/api/drives', { signal: controller.signal });
      clearTimeout(timeoutId);
      state.drives = Array.isArray(data) ? data : [];

      if (state.selectedRoots.size === 0) {
        state.drives.forEach(function (d) {
          if (C.driveDefaultSelected(d)) state.selectedRoots.add(d.letter);
        });
      } else {
        // Um disco WSL nunca fica marcado, mesmo vindo da Configuração salva.
        state.drives.forEach(function (d) {
          if (C.driveIsWSL(d)) state.selectedRoots.delete(d.letter);
        });
      }
      renderDrivesGrid();
    } catch (err) {
      clearTimeout(timeoutId);
      console.warn('Erro ao obter discos:', err);
      elements.drivesGrid.innerHTML =
        '<div class="empty-state drives-error">' +
          '<p class="drives-error-title">Não foi possível consultar as unidades de disco.</p>' +
          '<p class="drives-error-detail">' + esc(err.message) + '</p>' +
          '<button id="btnRetryDrives" class="btn btn-secondary btn-sm">🔄 Tentar Novamente</button>' +
        '</div>';
      var retry = document.getElementById('btnRetryDrives');
      if (retry) retry.addEventListener('click', fetchDrives);
    }
  }

  function renderDrivesGrid() {
    if (!elements.drivesGrid) return;
    if (!state.drives || state.drives.length === 0) {
      elements.drivesGrid.textContent = 'Nenhum disco detectado.';
      return;
    }

    elements.drivesGrid.innerHTML = state.drives.map(function (drive) {
      var isWSL = C.driveIsWSL(drive);
      var isSelected = state.selectedRoots.has(drive.letter);
      var usedPercent = Number(drive.usedPercent) || 0;
      var isHighUsage = usedPercent > 85;

      var warning = isWSL
        ? '<span class="drive-warning">⚠️ Volume do WSL: os pseudo-arquivos do Linux não são contabilizáveis e a Varredura pode travar. Marque apenas se souber o que está fazendo.</span>'
        : '';

      return '' +
        '<div class="drive-card ' + (isSelected ? 'selected' : '') + ' ' + (isWSL ? 'drive-wsl' : '') + '" data-letter="' + esc(drive.letter) + '">' +
          '<div class="drive-card-header">' +
            '<div class="drive-name-row">' +
              '<div class="drive-icon">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">' +
                  '<rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>' +
                  '<rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>' +
                  '<line x1="6" y1="6" x2="6.01" y2="6"></line>' +
                  '<line x1="6" y1="18" x2="6.01" y2="18"></line>' +
                '</svg>' +
              '</div>' +
              '<div>' +
                '<div class="drive-letter">' + esc(drive.letter) + '</div>' +
                '<div class="drive-label">' + esc(drive.volumeLabel || 'Disco Local') + ' (' + esc(drive.fileSystem || 'NTFS') + ')</div>' +
              '</div>' +
            '</div>' +
            '<input type="checkbox" class="drive-checkbox" ' + (isSelected ? 'checked' : '') + ' tabindex="-1" aria-hidden="true">' +
          '</div>' +
          '<div class="drive-meta-row">' +
            '<span>' + esc(drive.driveType || '') + (isWSL ? ' &bull; WSL' : '') + '</span>' +
            '<span>' + esc(usedPercent.toFixed(1)) + '% Usado</span>' +
          '</div>' +
          '<div class="drive-bar-track">' +
            '<div class="drive-bar-fill ' + (isHighUsage ? 'danger' : '') + '" style="width: ' + Math.min(100, usedPercent) + '%"></div>' +
          '</div>' +
          '<div class="drive-space-text">' +
            '<span>Livre: ' + esc(formatBytes(drive.freeBytes)) + '</span>' +
            '<span>Total: ' + esc(formatBytes(drive.totalBytes)) + '</span>' +
          '</div>' +
          warning +
        '</div>';
    }).join('');

    elements.drivesGrid.querySelectorAll('.drive-card').forEach(function (card) {
      card.addEventListener('click', function () {
        var letter = card.getAttribute('data-letter');
        if (state.selectedRoots.has(letter)) state.selectedRoots.delete(letter);
        else state.selectedRoots.add(letter);
        renderDrivesGrid();
        queueConfigChange({ selectedRoots: currentSelectedRootsArray() });
      });
    });
  }

  // ==========================================
  // VARREDURA: iniciar, cancelar e ressincronizar (contrato 1.2)
  // ==========================================

  async function startScan(isQuick) {
    if (state.selectedRoots.size === 0) {
      showToast('Selecione ao menos uma unidade de disco para varrer.', 'warning');
      return;
    }

    var payload = {
      roots: currentSelectedRootsArray(),
      workerThreads: elements.workerThreads ? (parseInt(elements.workerThreads.value, 10) || 0) : 0,
      hashAlgorithm: elements.hashAlgo ? elements.hashAlgo.value : 'xxhash',
      hashAllFiles: elements.hashMode ? elements.hashMode.value === 'all' : false,
      minSizeForHash: elements.minFileSize ? (parseInt(elements.minFileSize.value, 10) || 1) : 1,
      quickScanMode: !!isQuick,
      autoSaveIntervalMinutes: elements.autoSaveInterval ? (parseInt(elements.autoSaveInterval.value, 10) || 0) : 5,
      autoSaveOnComplete: true,
    };

    var res = await apiPostJSON('/api/scan/start', payload);

    if (res.status === 409) {
      // O servidor informa a fase que impede o início (contrato 1.2).
      var busyPhase = res.data && res.data.phase ? res.data.phase : state.phase;
      var info = C.phaseState(busyPhase);
      showToast('Já existe uma Varredura em andamento: ' + info.label + '.', 'warning');
      fetchScanStatus();
      return;
    }

    if (!res.ok) {
      showToast('Erro ao iniciar a Varredura: ' + res.error, 'danger');
      return;
    }

    if (elements.progressHUD) elements.progressHUD.classList.remove('hidden');
    if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
    applyPhaseToControls('phase1_metadata');
    showToast(isQuick ? '⚡ Quick Scan iniciado.' : 'Varredura Completa iniciada.', 'success');
    fetchScanStatus();
  }

  async function cancelScan() {
    var res = await apiPostJSON('/api/scan/cancel', {});
    if (!res.ok) {
      showToast('Não foi possível cancelar: ' + res.error, 'danger');
      return;
    }
    // O botão Cancelar continua visível até a fase virar `cancelled`.
    applyPhaseToControls('cancelling');
    showToast('Cancelamento solicitado. Aguardando o pipeline parar...', 'info');
  }

  /** Ressincroniza o estado com o servidor (ao carregar e a cada onopen). */
  async function fetchScanStatus() {
    try {
      var st = await apiGetJSON('/api/scan/status');
      if (st) updateScanProgress(st);
    } catch (err) {
      console.warn('Não foi possível ler /api/scan/status:', err);
    }
  }

  /** Coerência dos botões e rótulos a partir da fase (contrato 1.2). */
  function applyPhaseToControls(phase) {
    var info = C.phaseState(phase);
    state.phase = info.phase;
    state.isScanning = info.busy;

    if (elements.btnStartScan) elements.btnStartScan.classList.toggle('hidden', !info.canStart);
    if (elements.btnQuickScan) elements.btnQuickScan.classList.toggle('hidden', !info.canStart);
    if (elements.btnCancelScan) elements.btnCancelScan.classList.toggle('hidden', !info.canCancel);
    if (elements.scanPhaseText) elements.scanPhaseText.textContent = info.label;

    if (elements.liveBadge) elements.liveBadge.className = 'status-badge ' + info.badge;
    if (elements.liveStatusText) elements.liveStatusText.textContent = info.label;
  }

  // ==========================================
  // SSE (contrato 1.8)
  // ==========================================

  function setupSSE() {
    if (sessionInvalid) return;
    if (state.sseSource) {
      try { state.sseSource.close(); } catch (e) { /* já fechado */ }
    }

    var source = new EventSource(tokenQuery('/api/events'));
    state.sseSource = source;

    source.onopen = function () {
      // Toda reconexão ressincroniza o estado com o servidor.
      fetchScanStatus();
    };

    source.addEventListener('scan_progress', function (e) {
      var data = C.safeParseJSON(e.data, null);
      if (data) updateScanProgress(data);
    });

    source.addEventListener('autosave_done', function (e) {
      var data = C.safeParseJSON(e.data, null);
      if (data && elements.hudAutoSaveBadge) {
        var d = new Date((data.time || 0) * 1000);
        elements.hudAutoSaveBadge.textContent = '💾 Auto-Salvo (' + d.toLocaleTimeString('pt-BR') + ')';
        elements.hudAutoSaveBadge.classList.remove('hidden');
      }
      showToast('💾 Snapshot salvo automaticamente.', 'success');
    });

    source.addEventListener('fs_event', function (e) {
      var log = C.safeParseJSON(e.data, null);
      if (log) addFSEvent(log);
    });

    source.addEventListener('shutdown', function (e) {
      var data = C.safeParseJSON(e.data, null) || {};
      var segundos = Number(data.inSeconds) || 0;
      showToast('O servidor vai encerrar' + (segundos ? ' em ' + segundos + 's' : '') +
        (data.reason ? ': ' + data.reason : '.'), 'warning');
    });

    source.onerror = function () {
      // O EventSource reconecta sozinho; o onopen seguinte ressincroniza.
    };
  }

  // ==========================================
  // HUD DE PROGRESSO
  // ==========================================

  function updateScanProgress(st) {
    if (!st) return;
    state.scanStatus = st;
    state.lastProgressAt = Date.now();

    if (st.memoryStats) updateMemoryTelemetry(st.memoryStats);

    var phase = st.phase || 'idle';
    var info = C.phaseState(phase);
    applyPhaseToControls(phase);

    if (elements.progressHUD && (info.busy || phase === 'completed' || phase === 'cancelled' || phase === 'watching')) {
      elements.progressHUD.classList.remove('hidden');
    }

    setText(elements.statFiles, formatNumber(st.totalFilesScanned));
    setText(elements.statDirs, formatNumber(st.totalDirsScanned));
    setText(elements.statBytes, formatBytes(st.totalBytesScanned));
    setText(elements.statAllocatedBytes, formatBytes(st.totalAllocatedBytesScanned || st.totalBytesScanned));
    setText(elements.statCompressedCount, formatNumber(st.compressedFilesCount || 0));
    setText(elements.statSkipped, formatNumber(st.skippedCount || 0));
    setText(elements.statPrehash, formatNumber(st.prehashCount || 0));

    if (elements.statCompressedSaved) {
      var saved = st.compressedSpaceSavedBytes || 0;
      if (saved > 0) {
        var ratio = st.compressionRatio ? ' (' + st.compressionRatio.toFixed(2) + 'x)' : '';
        elements.statCompressedSaved.textContent = '+' + formatBytes(saved) + ratio;
        elements.statCompressedSaved.style.color = '#34d399';
      } else {
        elements.statCompressedSaved.textContent = '0 B';
        elements.statCompressedSaved.style.color = 'var(--text-muted)';
      }
    }

    setText(elements.currentPathText, st.currentPath || info.label);
    if (st.elapsedTimeSec > 0) setText(elements.statElapsed, Math.round(st.elapsedTimeSec) + 's');

    // Threads efetivas de cada fase (campos novos do contrato 1.2).
    if (elements.hudWorkersBadge) {
      if (st.phase1Workers || st.phase2Workers) {
        elements.hudWorkersBadge.textContent = 'Threads: ' + (st.phase1Workers || 0) + ' na Fase 1, ' + (st.phase2Workers || 0) + ' na Fase 2';
        elements.hudWorkersBadge.classList.remove('hidden');
      } else {
        elements.hudWorkersBadge.classList.add('hidden');
      }
    }

    if (st.isQuickScan) {
      toggleHidden(elements.hudQuickScanBadge, false);
      showBox(elements.boxReusedFiles, true);
      showBox(elements.boxNewModifiedFiles, true);
      setText(elements.statReusedFiles, formatNumber(st.reusedFilesCount || 0));
      setText(elements.statNewModifiedFiles, formatNumber((st.modifiedFilesCount || 0) + (st.newFilesCount || 0)));
    } else {
      toggleHidden(elements.hudQuickScanBadge, true);
      showBox(elements.boxReusedFiles, false);
      showBox(elements.boxNewModifiedFiles, false);
    }

    if (st.lastAutoSaveTime > 0 && elements.hudAutoSaveBadge) {
      var d = new Date(st.lastAutoSaveTime * 1000);
      elements.hudAutoSaveBadge.textContent = '💾 Auto-Salvo (' + d.toLocaleTimeString('pt-BR') + ')';
      elements.hudAutoSaveBadge.classList.remove('hidden');
    }

    if (elements.hudErrorsBadge) {
      var problemas = (st.errorsCount || 0) + (st.skippedCount || 0);
      if (problemas > 0) {
        elements.hudErrorsBadge.textContent = '⚠️ ' + formatNumber(problemas) + ' itens bloqueados ou pulados';
        elements.hudErrorsBadge.classList.remove('hidden');
      } else {
        elements.hudErrorsBadge.classList.add('hidden');
      }
    }

    updatePhasePresentation(st, phase, info);
    updateWatcherState(st);
    renderActiveWorkers(st);
    renderRecentFiles(st);
  }

  function updatePhasePresentation(st, phase, info) {
    if (!elements.hudPhaseBadge) return;

    if (phase === 'loading_cache') {
      setGlobalOperationLock(true, 'Carregando Snapshot para a memória...',
        st.currentPath || 'Reconstruindo a árvore e os índices...', st.progressPercent || 0, st.currentPath || '');
      elements.hudPhaseBadge.textContent = info.label;
      return;
    }

    setBadge(elements.hudPhaseBadge, info.label, phase);

    if (phase === 'phase1_metadata') {
      setText(elements.statSpeed, Math.round(st.scanRateFilesPerSec || 0) + ' arq/s');
      if (elements.progressBarFill) elements.progressBarFill.style.width = '40%';
      setText(elements.progressPercentText, 'Fase 1 em andamento');
      return;
    }

    if (phase === 'phase2_hashing') {
      setText(elements.statSpeed, (st.hashRateMBPerSec || 0).toFixed(1) + ' MB/s');
      var pct = Math.min(100, Math.max(0, st.progressPercent || 0));
      if (elements.progressBarFill) elements.progressBarFill.style.width = pct + '%';
      setText(elements.progressPercentText, pct.toFixed(1) + '% (' + formatNumber(st.filesHashedCount) + ' / ' + formatNumber(st.filesToHashCount) + ')');
      return;
    }

    if (phase === 'indexing') {
      if (elements.progressBarFill) elements.progressBarFill.style.width = '95%';
      setText(elements.progressPercentText, 'Construindo os índices de duplicados');
      return;
    }

    if (phase === 'cancelling') {
      setText(elements.progressPercentText, 'Parando o pipeline...');
      return;
    }

    if (phase === 'cancelled') {
      setGlobalOperationLock(false);
      if (elements.progressBarFill) elements.progressBarFill.style.width = '0%';
      setText(elements.progressPercentText, 'Cancelada');
      showToastOnce('scan_cancelled_' + (st.startTime || 0), 'Varredura cancelada. Nenhum autosave foi gravado.', 'info');
      return;
    }

    if (phase === 'completed' || phase === 'watching') {
      setGlobalOperationLock(false);
      if (elements.progressBarFill) elements.progressBarFill.style.width = '100%';
      setText(elements.progressPercentText, '100%');
      updateDuplicateBadges(st);
      refreshActiveTabAfterScan();
    }
  }

  var shownToasts = new Set();
  function showToastOnce(key, message, type) {
    if (shownToasts.has(key)) return;
    shownToasts.add(key);
    showToast(message, type);
  }

  function updateDuplicateBadges(st) {
    if (elements.dupCountBadge) {
      if (st.duplicateGroupsCount > 0) {
        elements.dupCountBadge.textContent = formatNumber(st.duplicateGroupsCount);
        elements.dupCountBadge.classList.remove('hidden');
      } else {
        elements.dupCountBadge.classList.add('hidden');
      }
    }
    setText(elements.dupTotalGroups, formatNumber(st.duplicateGroupsCount));
    setText(elements.dupTotalFiles, formatNumber(st.duplicateFilesCount));
    setText(elements.dupTotalWasted, formatBytes(st.duplicateWastedBytes));

    if (elements.folderDupCountBadge) {
      if (st.duplicateFolderGroupsCount > 0) {
        elements.folderDupCountBadge.textContent = formatNumber(st.duplicateFolderGroupsCount);
        elements.folderDupCountBadge.classList.remove('hidden');
      } else {
        elements.folderDupCountBadge.classList.add('hidden');
      }
    }
    setText(elements.dupFolderTotalGroups, formatNumber(st.duplicateFolderGroupsCount));
    setText(elements.dupFolderTotalCount, formatNumber(st.duplicateFoldersCount));
    setText(elements.dupFolderTotalWasted, formatBytes(st.duplicateFolderWastedBytes));
  }

  function refreshActiveTabAfterScan() {
    if (state.currentTab === 'treeTab') loadTreeData(state.treePath);
    else if (state.currentTab === 'duplicatesTab') loadDuplicates(state.dupPage);
    else if (state.currentTab === 'foldersTab') loadFolderDuplicates(state.folderDupPage);
    else if (state.currentTab === 'analyticsTab') loadAnalytics();
  }

  function updateWatcherState(st) {
    if (!elements.watcherStateTitle || !elements.watcherStateDesc) return;
    if (st.isWatching) {
      elements.watcherStateTitle.textContent = 'Monitoramento ativo';
      elements.watcherStateDesc.textContent = 'Observando recursivamente as Raízes Varridas e atualizando a árvore e os índices.';
    } else {
      elements.watcherStateTitle.textContent = 'Monitoramento inativo';
      elements.watcherStateDesc.textContent = 'O Monitoramento começa depois de uma Varredura Completa.';
    }
  }

  function setText(el, value) {
    if (el) el.textContent = value === undefined || value === null ? '' : String(value);
  }

  function toggleHidden(el, hidden) {
    if (el) el.classList.toggle('hidden', !!hidden);
  }

  function showBox(el, visible) {
    if (el) el.style.display = visible ? 'flex' : 'none';
  }

  var PHASE_COLORS = {
    phase1_metadata: ['rgba(56, 189, 248, 0.15)', '#38bdf8'],
    phase2_hashing: ['rgba(168, 85, 247, 0.15)', '#a855f7'],
    indexing: ['rgba(56, 189, 248, 0.15)', '#38bdf8'],
    cancelling: ['rgba(245, 158, 11, 0.15)', '#f59e0b'],
    cancelled: ['rgba(148, 163, 184, 0.15)', '#94a3b8'],
    completed: ['rgba(16, 185, 129, 0.15)', '#10b981'],
    watching: ['rgba(16, 185, 129, 0.15)', '#10b981'],
    idle: ['rgba(148, 163, 184, 0.15)', '#94a3b8'],
  };

  function setBadge(el, label, phase) {
    if (!el) return;
    el.textContent = label;
    var colors = PHASE_COLORS[phase] || PHASE_COLORS.idle;
    el.style.background = colors[0];
    el.style.color = colors[1];
  }

  function renderActiveWorkers(st) {
    if (!elements.activeWorkersSection || !elements.activeWorkersGrid) return;
    var workers = Array.isArray(st.activeWorkers) ? st.activeWorkers : [];
    var mostrando = workers.length > 0 && (st.phase === 'phase2_hashing' || st.phase === 'phase1_metadata');

    if (!mostrando) {
      elements.activeWorkersSection.classList.add('hidden');
      return;
    }

    elements.activeWorkersSection.classList.remove('hidden');
    setText(elements.activeWorkerCountText, workers.length + ' threads ativas');

    elements.activeWorkersGrid.innerHTML = workers.map(function (w) {
      var pct = (w.percent || 0).toFixed(1);
      var fileName = C.basename(w.path || '') || (w.path || '');
      var corpo = w.totalSize > 0
        ? '<div class="worker-progress-bar"><div class="worker-progress-fill" style="width: ' + pct + '%"></div></div>' +
          '<div class="worker-stat-text"><span>' + esc(formatBytes(w.bytesDone)) + ' / ' + esc(formatBytes(w.totalSize)) + '</span><span>' + esc(pct) + '%</span></div>'
        : '<div class="worker-stat-text"><span class="truncate">' + esc(w.path || '') + '</span></div>';

      return '' +
        '<div class="worker-card">' +
          '<div class="worker-card-header">' +
            '<span>Thread ' + esc((w.workerId || 0) + 1) + '</span>' +
            '<span>' + (w.totalSize > 0 ? esc(formatBytes(w.totalSize)) : 'Diretório') + '</span>' +
          '</div>' +
          '<div class="worker-file-name" title="' + esc(w.path || '') + '">' + esc(fileName) + '</div>' +
          corpo +
        '</div>';
    }).join('');
  }

  function renderRecentFiles(st) {
    if (!elements.recentFilesTableBody) return;
    var files = Array.isArray(st.recentFiles) ? st.recentFiles : [];
    if (files.length === 0) return;

    elements.recentFilesTableBody.innerHTML = files.slice().reverse().map(function (rf) {
      var timeStr = rf.timestamp ? new Date(rf.timestamp).toLocaleTimeString('pt-BR') : '-';
      var hashDisplay = rf.hash
        ? (rf.hash.length > 18 ? rf.hash.substring(0, 18) + '...' : rf.hash)
        : (rf.durationMs ? rf.durationMs + 'ms' : '-');
      var status = rf.status || 'OK';
      var mensagem = rf.message ? '<small class="recent-file-msg">' + esc(rf.message) + '</small>' : '';

      return '' +
        '<tr>' +
          '<td>' + esc(timeStr) + '</td>' +
          '<td><span class="status-pill status-' + esc(status) + '">' + esc(status) + '</span></td>' +
          '<td><strong>' + (rf.size > 0 ? esc(formatBytes(rf.size)) : '-') + '</strong></td>' +
          '<td class="truncate" title="' + esc(rf.path || '') + '">' + esc(rf.path || '') + mensagem + '</td>' +
          '<td><code class="recent-file-hash">' + esc(hashDisplay) + '</code></td>' +
        '</tr>';
    }).join('');
  }

  // ==========================================
  // AUTOSAVE
  // ==========================================

  async function checkAutoSaveStatus() {
    var data = null;
    try {
      data = await apiGetJSON('/api/cache/autosave/status');
    } catch (err) {
      return;
    }
    if (!data || !data.exists || !data.autoSave || !elements.autoSaveBanner) return;

    var info = data.autoSave;
    if (elements.autoSaveDetailsText) {
      elements.autoSaveDetailsText.textContent =
        'Arquivo: ' + (info.fileName || 'autosave_latest.sfz') + ' (' + formatBytes(info.sizeBytes) + ') • Salvo ' +
        C.formatTimeAgo(new Date(info.modTime));
    }
    elements.autoSaveBanner.classList.remove('hidden');
  }

  async function restoreAutoSave() {
    setGlobalOperationLock(true, 'Restaurando o Autosave...',
      'Lendo o arquivo compactado e reconstruindo a árvore em memória...', 10, 'Iniciando a leitura...');

    try {
      var res = await apiPostJSON('/api/cache/autosave/restore', {});
      if (!res.ok) throw new Error(res.error);

      // O contrato 1.7 devolve apenas `summary`; `snapshot` é o nome legado.
      var summary = (res.data && (res.data.summary || res.data.snapshot)) || {};
      updateGlobalProgress(100, 'Autosave restaurado.');
      if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
      showToast('Autosave restaurado: ' + formatNumber(summary.totalFiles) + ' arquivos.', 'success');
      applySnapshotSummary(summary);
      return summary;
    } catch (err) {
      showToast('Erro ao restaurar o autosave: ' + err.message, 'danger');
      throw err;
    } finally {
      setTimeout(function () { setGlobalOperationLock(false); }, 350);
    }
  }

  function applySnapshotSummary(summary) {
    if (!summary) return;

    if (Array.isArray(summary.roots) && summary.roots.length > 0) {
      state.selectedRoots = new Set(summary.roots);
      renderDrivesGrid();
    }

    setText(elements.statFiles, formatNumber(summary.totalFiles));
    setText(elements.statDirs, formatNumber(summary.totalDirs));
    setText(elements.statBytes, formatBytes(summary.totalBytes));

    loadTreeData('');
    loadDuplicates(1);
    loadFolderDuplicates(1);
    loadAnalytics();
  }

  // ==========================================
  // MONITOR DO SO: eventos e Itens Pulados (contrato 1.10)
  // ==========================================

  async function fetchEventLogs() {
    try {
      var logs = await apiGetJSON('/api/logs');
      state.eventLogs = Array.isArray(logs) ? logs.slice(-200) : [];
    } catch (err) {
      console.warn('Não foi possível ler /api/logs:', err);
      state.eventLogs = [];
    }
    renderEventLogs();
  }

  /** Recebe um FSEventLog do SSE e o coloca no topo da lista. */
  function addFSEvent(eventLog) {
    if (!eventLog) return;
    state.eventLogs.push(eventLog);
    if (state.eventLogs.length > 200) {
      state.eventLogs = state.eventLogs.slice(state.eventLogs.length - 200);
    }
    renderEventLogs();
  }

  var OP_LABELS = {
    CREATE: 'Criado',
    WRITE: 'Alterado',
    REMOVE: 'Removido',
    RENAME: 'Renomeado',
  };

  function renderEventLogs() {
    if (elements.eventCountBadge) {
      elements.eventCountBadge.textContent = formatNumber(state.eventLogs.length);
    }
    if (!elements.eventLogsTableBody) return;

    if (state.eventLogs.length === 0) {
      elements.eventLogsTableBody.innerHTML =
        '<tr><td colspan="4" class="empty-state">Aguardando eventos do sistema operacional...</td></tr>';
      return;
    }

    var recentes = state.eventLogs.slice().reverse();
    elements.eventLogsTableBody.innerHTML = recentes.map(function (ev) {
      var quando = ev.timestamp ? new Date(ev.timestamp).toLocaleTimeString('pt-BR') : '-';
      var op = String(ev.op || '');
      var delta = Number(ev.sizeDelta) || 0;
      var deltaTxt = delta === 0 ? '-' : (delta > 0 ? '+' : '') + formatBytes(delta);

      return '' +
        '<tr>' +
          '<td>' + esc(quando) + '</td>' +
          '<td><span class="status-pill status-' + esc(op || 'OK') + '">' + esc(OP_LABELS[op] || op || '-') + '</span></td>' +
          '<td class="truncate" title="' + esc(ev.path || '') + '">' + esc(ev.path || '') + '</td>' +
          '<td>' + esc(deltaTxt) + '</td>' +
        '</tr>';
    }).join('');
  }

  async function fetchSkippedItems() {
    try {
      var list = await apiGetJSON('/api/logs/skipped?limit=200');
      state.skippedItems = Array.isArray(list) ? list : [];
    } catch (err) {
      console.warn('Não foi possível ler /api/logs/skipped:', err);
      state.skippedItems = [];
    }
    renderSkippedItems();
  }

  function renderSkippedItems() {
    if (elements.skippedCountBadge) {
      elements.skippedCountBadge.textContent = formatNumber(state.skippedItems.length) + ' itens';
    }
    if (!elements.skippedItemsTableBody) return;

    if (state.skippedItems.length === 0) {
      elements.skippedItemsTableBody.innerHTML =
        '<tr><td colspan="3" class="empty-state">Nenhum item pulado registrado nesta execução.</td></tr>';
      return;
    }

    elements.skippedItemsTableBody.innerHTML = state.skippedItems.slice().reverse().map(function (item) {
      var quando = item.timestamp ? new Date(item.timestamp).toLocaleTimeString('pt-BR') : '-';
      return '' +
        '<tr>' +
          '<td>' + esc(quando) + '</td>' +
          '<td class="truncate" title="' + esc(item.path || '') + '">' + esc(item.path || '') + '</td>' +
          '<td class="skip-reason">' + esc(item.reason || '-') + '</td>' +
        '</tr>';
    }).join('');
  }

  // ==========================================
  // CORES DO TREEMAP
  // ==========================================

  var LEVEL_COLORS = [
    '#1e3a8a', '#0284c7', '#06b6d4', '#0d9488', '#10b981',
    '#eab308', '#f97316', '#ef4444', '#a855f7',
  ];

  var WINDIRSTAT_EXT_COLORS = {
    vhdx: '#2563eb', vhd: '#1d4ed8', vmdk: '#1e40af', vdi: '#3b82f6', img: '#2563eb', iso: '#b45309',
    zip: '#d97706', rar: '#f59e0b', '7z': '#b45309', tar: '#d97706', gz: '#f59e0b', bz2: '#b45309', cab: '#ca8a04', wim: '#d97706',
    dll: '#06b6d4', sys: '#10b981', drv: '#0d9488', ocx: '#14b8a6', cpl: '#059669',
    psarc: '#a855f7', pak: '#9333ea', bundle: '#c084fc', bin: '#7e22ce', dat: '#6b21a8', rpf: '#a855f7',
    exe: '#3b82f6', msi: '#2563eb', bat: '#0284c7', cmd: '#0369a1', ps1: '#0284c7',
    jpg: '#ef4444', jpeg: '#dc2626', png: '#f43f5e', webp: '#e11d48', gif: '#fb7185', bmp: '#b91c1c', tiff: '#be123c', psd: '#e11d48', raw: '#be123c',
    mp4: '#8b5cf6', mkv: '#7c3aed', avi: '#6d28d9', mov: '#a78bfa', wmv: '#5b21b6',
    mp3: '#84cc16', flac: '#65a30d', wav: '#4d7c0f', m4a: '#a3e635',
    pdf: '#dc2626', docx: '#0284c7', doc: '#0369a1', xlsx: '#16a34a', xls: '#15803d', pptx: '#ea580c', txt: '#64748b',
    sqlite: '#0ea5e9', db: '#0284c7', sql: '#0369a1', js: '#eab308', ts: '#3b82f6', go: '#06b6d4', py: '#3b82f6',
  };

  function getExtensionColor(ext) {
    if (!ext) return '#64748b';
    var clean = String(ext).replace(/^\./, '').toLowerCase();
    if (WINDIRSTAT_EXT_COLORS[clean]) return WINDIRSTAT_EXT_COLORS[clean];
    var hash = 0;
    for (var i = 0; i < clean.length; i++) {
      hash = clean.charCodeAt(i) + ((hash << 5) - hash);
    }
    return 'hsl(' + (Math.abs(hash) % 360) + ', 72%, 50%)';
  }

  function getNodeColor(node, colorMode, level) {
    if (colorMode === 'depth') return LEVEL_COLORS[level % LEVEL_COLORS.length];

    if (colorMode === 'age') {
      var nowSec = Date.now() / 1000;
      var ageDays = Math.max(0, Math.floor((nowSec - (node.modTime || nowSec)) / 86400));
      if (ageDays < 30) return '#10b981';
      if (ageDays < 180) return '#06b6d4';
      if (ageDays < 365) return '#3b82f6';
      if (ageDays < 730) return '#eab308';
      if (ageDays < 1825) return '#f97316';
      return '#ef4444';
    }

    if (node.isFile) {
      var ext = String(node.name || '').split('.').pop().toLowerCase();
      return getExtensionColor(ext);
    }
    return LEVEL_COLORS[level % LEVEL_COLORS.length];
  }

  // ==========================================
  // ÁRVORE (contrato 1.4)
  // ==========================================

  async function loadTreeData(path) {
    path = path || '';
    state.treePath = path;
    renderBreadcrumbs(path);

    if (elements.treemapCurrentTitle) {
      elements.treemapCurrentTitle.textContent = path ? 'Gráfico: ' + path : 'Gráfico da Estrutura (Todos os Discos)';
    }

    try {
      var depth = state.treemap.depth || 5;
      var url = '/api/tree?path=' + encodeURIComponent(path) + '&depth=' + depth;
      var data = await apiGetJSON(url);
      state.treeData = data || [];
      state.treemap.rawTree = state.treeData;

      await loadTreeFilesPage(1);
      resizeTreemapCanvas();
      renderTreemapLegend();
    } catch (err) {
      if (elements.treeTableBody) {
        elements.treeTableBody.innerHTML =
          '<tr><td colspan="8" class="empty-state">' + esc(err.message) + '</td></tr>';
      }
    }
  }

  /**
   * Página de arquivos da pasta atual via /api/tree/files (100 por página).
   * Enquanto o endpoint não existir, cai para os arquivos que /api/tree trouxe
   * (que o contrato 1.4 limita aos 500 maiores).
   */
  async function loadTreeFilesPage(page) {
    state.treeFilesPage = Math.max(1, page || 1);

    var dados = state.treeData;
    var ehRaiz = Array.isArray(dados);

    if (ehRaiz || !dados) {
      state.treeFiles = [];
      state.treeFilesTotal = 0;
      renderTreeTable();
      return;
    }

    var fallbackFiles = Array.isArray(dados.files) ? dados.files : [];
    var totalConhecido = dados.fileCount !== undefined ? Number(dados.fileCount) : fallbackFiles.length;

    if (!state.treeFilesSupported) {
      state.treeFiles = fallbackFiles;
      state.treeFilesTotal = fallbackFiles.length;
      renderTreeTable();
      return;
    }

    var bounds = C.pageBounds(totalConhecido, state.treeFilesPage, state.treeFilesLimit);
    var url = '/api/tree/files?path=' + encodeURIComponent(state.treePath) +
      '&offset=' + bounds.offset + '&limit=' + state.treeFilesLimit +
      '&sortBy=' + encodeURIComponent(state.treeFilesSortBy);

    try {
      var res = await apiFetch(url);
      if (res.status === 404) {
        // Servidor antigo: segue com o que /api/tree entregou.
        state.treeFilesSupported = false;
        state.treeFiles = fallbackFiles;
        state.treeFilesTotal = fallbackFiles.length;
        renderTreeTable();
        return;
      }
      if (!res.ok) throw new Error(await readError(res));

      var payload = C.safeParseJSON(await res.text(), null) || {};
      state.treeFiles = Array.isArray(payload.files) ? payload.files : [];
      state.treeFilesTotal = Number(payload.total) || state.treeFiles.length;
    } catch (err) {
      console.warn('Falha em /api/tree/files:', err);
      state.treeFiles = fallbackFiles;
      state.treeFilesTotal = fallbackFiles.length;
    }

    renderTreeTable();
  }

  function treeGoUp() {
    if (!state.treePath) return;
    var pai = C.parentPath(state.treePath);
    if (!pai || pai === state.treePath) {
      loadTreeData('');
      return;
    }
    loadTreeData(pai);
  }

  function renderBreadcrumbs(path) {
    if (!elements.treeBreadcrumbs) return;

    if (!path) {
      elements.treeBreadcrumbs.innerHTML = '<span class="breadcrumb-item active" data-path="">Meus Discos</span>';
      bindBreadcrumbs();
      return;
    }

    var parts = String(path).split(/[\/\\]/).filter(Boolean);
    var html = '<span class="breadcrumb-item" data-path="">Meus Discos</span>';
    var accum = '';

    parts.forEach(function (p, idx) {
      accum = idx === 0 ? p + '\\' : accum + '\\' + p;
      var isLast = idx === parts.length - 1;
      html += ' / <span class="breadcrumb-item ' + (isLast ? 'active' : '') + '" data-path="' + esc(accum) + '">' + esc(p) + '</span>';
    });

    elements.treeBreadcrumbs.innerHTML = html;
    bindBreadcrumbs();
  }

  function bindBreadcrumbs() {
    elements.treeBreadcrumbs.querySelectorAll('.breadcrumb-item').forEach(function (b) {
      b.addEventListener('click', function () {
        loadTreeData(b.getAttribute('data-path'));
      });
    });
  }

  function renderTreeTable() {
    if (!elements.treeTableBody) return;

    var items = [];
    var parentSize = 1;
    var ehRaiz = Array.isArray(state.treeData);

    if (ehRaiz) {
      items = (state.treeData || []).map(function (r) {
        return {
          path: r.path,
          name: '💾 Unidade (' + (r.name || r.path) + ')',
          totalSize: r.totalSize || 0,
          totalAllocatedSize: r.totalAllocatedSize || r.totalSize || 0,
          fileCount: r.fileCount || 0,
          subDirCount: r.subDirCount || 0,
          modTime: r.modTime,
          createTime: r.createTime,
          isFile: false,
        };
      });
      parentSize = items.reduce(function (acc, cur) { return acc + (cur.totalSize || 0); }, 0) || 1;
    } else if (state.treeData) {
      parentSize = state.treeData.totalSize || 1;

      (state.treeData.subDirs || []).forEach(function (d) {
        if (d) items.push(d);
      });

      state.treeFiles.forEach(function (f) {
        items.push({
          path: f.path,
          name: f.name,
          totalSize: f.size,
          totalAllocatedSize: f.allocatedSize || f.size,
          isCompressed: f.isCompressed,
          fileCount: 1,
          subDirCount: 0,
          createTime: f.createTime,
          modTime: f.modTime,
          isFile: true,
          isSymlink: f.isSymlink,
          linkTarget: f.linkTarget,
        });
      });
    }

    var search = elements.treeSearchInput ? elements.treeSearchInput.value.toLowerCase().trim() : '';
    if (search) {
      items = items.filter(function (it) {
        return String(it.name || '').toLowerCase().indexOf(search) >= 0;
      });
    }

    if (items.length === 0) {
      elements.treeTableBody.innerHTML = '<tr><td colspan="8" class="empty-state">Nenhum item nesta pasta.</td></tr>';
      if (elements.treePaginationBar) elements.treePaginationBar.textContent = '';
      updateTreeFilesHint();
      return;
    }

    elements.treeTableBody.innerHTML = items.map(function (item) {
      var pct = parentSize > 0 ? Math.min(100, (item.totalSize / parentSize) * 100).toFixed(1) : '0.0';
      var isDir = !item.isFile;
      var allocated = item.totalAllocatedSize !== undefined ? item.totalAllocatedSize : (item.totalSize || 0);

      var compressionBadge = item.isCompressed
        ? '<span class="badge badge-ntfs" title="Arquivo comprimido no NTFS">📦 NTFS</span>'
        : '';

      var iconSvg = isDir
        ? '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>'
        : '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>';

      var linkBadge = '';
      if (item.isSymlink) {
        linkBadge = '<span class="badge badge-link" title="Link simbólico ou junção">🔗 Link</span>';
        if (item.linkTarget) {
          linkBadge += '<span class="link-target" title="' + esc(item.linkTarget) + '">➔ ' + esc(item.linkTarget) + '</span>';
        }
      }

      return '' +
        '<tr class="' + (isDir ? 'dir-row' : 'file-row') + '" data-path="' + esc(item.path) + '">' +
          '<td>' +
            '<div class="tree-node-cell">' +
              '<span class="tree-icon ' + (isDir ? '' : 'file-icon') + '">' + iconSvg + '</span>' +
              '<span class="tree-node-name ' + (isDir ? 'clickable-dir' : '') + '">' + esc(item.name) + '</span>' +
              linkBadge +
            '</div>' +
          '</td>' +
          '<td><strong>' + esc(formatBytes(item.totalSize)) + '</strong></td>' +
          '<td><span class="' + (allocated < item.totalSize ? 'size-compressed' : '') + '">' + esc(formatBytes(allocated)) + '</span>' + compressionBadge + '</td>' +
          '<td><div class="size-meter-cell"><div class="size-meter-bar"><div class="size-meter-fill" style="width: ' + pct + '%"></div></div><span>' + esc(pct) + '%</span></div></td>' +
          '<td>' + (isDir ? esc(formatNumber(item.fileCount)) : '-') + '</td>' +
          '<td>' + (isDir ? esc(formatNumber(item.subDirCount)) : '-') + '</td>' +
          '<td>' + esc(formatDate(item.createTime)) + '</td>' +
          '<td>' + esc(formatDate(item.modTime)) + '</td>' +
        '</tr>';
    }).join('');

    elements.treeTableBody.querySelectorAll('.dir-row').forEach(function (el) {
      el.addEventListener('click', function () {
        loadTreeData(el.getAttribute('data-path'));
      });
    });

    updateTreeFilesHint();

    if (elements.treePaginationBar) {
      if (!ehRaiz && state.treeFilesTotal > state.treeFilesLimit) {
        renderPaginationBar(
          elements.treePaginationBar,
          state.treeFilesTotal,
          state.treeFilesPage,
          state.treeFilesLimit,
          function (newPage) { loadTreeFilesPage(newPage); },
          null,
          'arquivos'
        );
      } else {
        elements.treePaginationBar.textContent = '';
      }
    }
  }

  function updateTreeFilesHint() {
    if (!elements.treeFilesHint) return;
    if (Array.isArray(state.treeData) || !state.treeData) {
      elements.treeFilesHint.textContent = 'Selecione uma unidade para navegar pelas pastas.';
      return;
    }
    if (!state.treeFilesSupported) {
      elements.treeFilesHint.textContent =
        'Mostrando os ' + formatNumber(state.treeFiles.length) + ' maiores arquivos entregues por /api/tree.';
      return;
    }
    elements.treeFilesHint.textContent =
      'Subpastas listadas por inteiro; ' + formatNumber(state.treeFilesTotal) +
      ' arquivos paginados de ' + state.treeFilesLimit + ' em ' + state.treeFilesLimit + ' pelo servidor.';
  }

  // ==========================================
  // TREEMAP: canvas, layout e interação
  // ==========================================

  /** Ponto do canvas em coordenadas de layout, válido em qualquer zoom. */
  function canvasPointFromEvent(e) {
    var rect = elements.treemapCanvas.getBoundingClientRect();
    return C.mapClientToCanvas(e.clientX, e.clientY, rect, state.treemap.layoutWidth, state.treemap.layoutHeight);
  }

  function findNodeAtEvent(e) {
    var p = canvasPointFromEvent(e);
    return C.hitTest(state.treemap.layoutNodes, p.x, p.y);
  }

  function setupTreemap() {
    if (!elements.treemapCanvas || !elements.treemapContainer) return;

    window.addEventListener('resize', debounce(resizeTreemapCanvas, 100));

    elements.treemapCanvas.addEventListener('mousemove', function (e) {
      var found = findNodeAtEvent(e);

      if (found !== state.treemap.hoveredNode) {
        state.treemap.hoveredNode = found;
        renderTreemapCanvas();
      }

      if (!found || !elements.treemapTooltip) {
        if (elements.treemapTooltip) elements.treemapTooltip.classList.add('hidden');
        return;
      }

      renderTreemapTooltip(found, e);
    });

    elements.treemapCanvas.addEventListener('mouseleave', function () {
      state.treemap.hoveredNode = null;
      if (elements.treemapTooltip) elements.treemapTooltip.classList.add('hidden');
      renderTreemapCanvas();
    });

    elements.treemapCanvas.addEventListener('click', function (e) {
      if (elements.treemapContextMenu) elements.treemapContextMenu.classList.add('hidden');
      var found = findNodeAtEvent(e);
      state.treemap.selectedNode = found;
      renderTreemapCanvas();

      if (found && elements.treeTableBody) {
        var alvo = elements.treeTableBody.querySelector('tr[data-path="' + CSS.escape(found.node.path || '') + '"]');
        if (alvo) {
          elements.treeTableBody.querySelectorAll('tr').forEach(function (r) { r.classList.remove('row-highlight'); });
          alvo.classList.add('row-highlight');
          alvo.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }
    });

    elements.treemapCanvas.addEventListener('dblclick', function (e) {
      var found = findNodeAtEvent(e);
      if (!found) return;
      if (!found.node.isFile) {
        loadTreeData(found.node.path);
        return;
      }
      var pai = C.parentPath(found.node.path);
      if (pai) loadTreeData(pai);
    });

    elements.treemapCanvas.addEventListener('contextmenu', function (e) {
      e.preventDefault();
      var found = findNodeAtEvent(e);
      state.treemap.contextNode = found;
      if (!found || !elements.treemapContextMenu) return;

      var rect = elements.treemapCanvas.getBoundingClientRect();
      var escala = C.zoomScaleOf(rect, state.treemap.layoutWidth) || 1;
      var posX = (e.clientX - rect.left) / escala;
      var posY = (e.clientY - rect.top) / escala;
      var largura = state.treemap.layoutWidth;
      var altura = state.treemap.layoutHeight;

      if (posX + 230 > largura) posX = Math.max(10, largura - 240);
      if (posY + 160 > altura) posY = Math.max(10, altura - 170);

      elements.treemapContextMenu.classList.remove('hidden');
      elements.treemapContextMenu.style.left = posX + 'px';
      elements.treemapContextMenu.style.top = posY + 'px';
    });

    document.addEventListener('click', function (e) {
      if (elements.treemapContextMenu && !elements.treemapContextMenu.contains(e.target)) {
        elements.treemapContextMenu.classList.add('hidden');
      }
    });

    if (elements.ctxZoomIn) {
      elements.ctxZoomIn.addEventListener('click', function () {
        if (!state.treemap.contextNode) return;
        var n = state.treemap.contextNode.node;
        elements.treemapContextMenu.classList.add('hidden');
        if (!n.isFile) loadTreeData(n.path);
        else {
          var pai = C.parentPath(n.path);
          if (pai) loadTreeData(pai);
        }
      });
    }

    if (elements.ctxZoomOut) {
      elements.ctxZoomOut.addEventListener('click', function () {
        elements.treemapContextMenu.classList.add('hidden');
        treeGoUp();
      });
    }

    if (elements.ctxCopyPath) {
      elements.ctxCopyPath.addEventListener('click', function () {
        if (!state.treemap.contextNode) return;
        navigator.clipboard.writeText(state.treemap.contextNode.node.path || '');
        showToast('Caminho copiado.', 'success');
        elements.treemapContextMenu.classList.add('hidden');
      });
    }

    if (elements.ctxRecycle) {
      elements.ctxRecycle.addEventListener('click', function () {
        if (!state.treemap.contextNode) return;
        var n = state.treemap.contextNode.node;
        elements.treemapContextMenu.classList.add('hidden');
        requestRecycleNode(n);
      });
    }

    setupSplitter();
    setupMaxHeightToggle();
  }

  function renderTreemapTooltip(found, e) {
    var raiz = state.treeData;
    var parentTotal = 1;
    if (Array.isArray(raiz)) {
      parentTotal = raiz.reduce(function (a, b) { return a + (b.totalSize || 0); }, 0) || 1;
    } else if (raiz) {
      parentTotal = raiz.totalSize || 1;
    }

    var node = found.node;
    var pct = ((node.totalSize / parentTotal) * 100).toFixed(1);
    var isDir = !node.isFile;

    var extra = isDir
      ? '<div class="treemap-tooltip-metric"><span>Arquivos:</span><strong>' + esc(formatNumber(node.fileCount)) + '</strong></div>' +
        '<div class="treemap-tooltip-metric"><span>Subpastas:</span><strong>' + esc(formatNumber(node.subDirCount)) + '</strong></div>'
      : '';

    elements.treemapTooltip.innerHTML = '' +
      '<div class="treemap-tooltip-title">' + (isDir ? '📁 ' : '📄 ') + esc(node.name) + '</div>' +
      '<div class="treemap-tooltip-metric"><span>Caminho:</span><strong class="truncate tooltip-path">' + esc(node.path) + '</strong></div>' +
      '<div class="treemap-tooltip-metric"><span>Tamanho:</span><strong>' + esc(formatBytes(node.totalSize)) + ' (' + esc(pct) + '%)</strong></div>' +
      extra +
      '<div class="treemap-tooltip-metric"><span>Criado em:</span><span>' + esc(formatDate(node.createTime)) + '</span></div>' +
      '<div class="treemap-tooltip-metric"><span>Modificado em:</span><span>' + esc(formatDate(node.modTime)) + '</span></div>';

    elements.treemapTooltip.classList.remove('hidden');

    var rect = elements.treemapCanvas.getBoundingClientRect();
    var escala = C.zoomScaleOf(rect, state.treemap.layoutWidth) || 1;
    var tipX = (e.clientX - rect.left) / escala + 15;
    var tipY = (e.clientY - rect.top) / escala + 15;
    var tipW = 280;
    var tipH = 150;

    if (tipX + tipW > state.treemap.layoutWidth) tipX = Math.max(10, state.treemap.layoutWidth - tipW - 10);
    if (tipY + tipH > state.treemap.layoutHeight) tipY = Math.max(10, state.treemap.layoutHeight - tipH - 10);

    elements.treemapTooltip.style.left = tipX + 'px';
    elements.treemapTooltip.style.top = tipY + 'px';
  }

  function setupSplitter() {
    if (!elements.treeSplitter || !elements.treeSplitLayout) return;
    var isDragging = false;

    try {
      var savedRatio = localStorage.getItem('scanfile_split_left');
      if (savedRatio) {
        var left = parseFloat(savedRatio);
        if (left >= 0.2 && left <= 0.8) {
          elements.treeSplitLayout.style.setProperty('--tree-left-flex', left.toFixed(3));
          elements.treeSplitLayout.style.setProperty('--tree-right-flex', (1 - left).toFixed(3));
        }
      }
    } catch (e) { /* modo privado */ }

    elements.treeSplitter.addEventListener('pointerdown', function (e) {
      isDragging = true;
      elements.treeSplitter.classList.add('dragging');
      elements.treeSplitter.setPointerCapture(e.pointerId);
      document.body.style.userSelect = 'none';
      document.body.style.cursor = 'col-resize';
    });

    elements.treeSplitter.addEventListener('pointermove', function (e) {
      if (!isDragging) return;
      var rect = elements.treeSplitLayout.getBoundingClientRect();
      if (rect.width <= 0) return;
      var leftRatio = Math.max(0.2, Math.min(0.8, (e.clientX - rect.left) / rect.width));

      elements.treeSplitLayout.style.setProperty('--tree-left-flex', leftRatio.toFixed(3));
      elements.treeSplitLayout.style.setProperty('--tree-right-flex', (1 - leftRatio).toFixed(3));
      try { localStorage.setItem('scanfile_split_left', leftRatio.toFixed(3)); } catch (err) { /* modo privado */ }
      resizeTreemapCanvas();
    });

    var stopDrag = function (e) {
      if (!isDragging) return;
      isDragging = false;
      elements.treeSplitter.classList.remove('dragging');
      try { elements.treeSplitter.releasePointerCapture(e.pointerId); } catch (err) { /* já solto */ }
      document.body.style.userSelect = '';
      document.body.style.cursor = '';
      resizeTreemapCanvas();
    };

    elements.treeSplitter.addEventListener('pointerup', stopDrag);
    elements.treeSplitter.addEventListener('pointercancel', stopDrag);
  }

  function setupMaxHeightToggle() {
    if (!elements.btnTreeMaxHeight) return;

    var update = function (isMax) {
      elements.btnTreeMaxHeight.textContent = isMax ? '🗗 Restaurar Altura' : '⛶ Tela Máxima';
      elements.btnTreeMaxHeight.title = isMax ? 'Restaurar o layout padrão' : 'Expandir a altura máxima na tela';
      elements.btnTreeMaxHeight.classList.toggle('active', isMax);
    };

    var saved = false;
    try { saved = localStorage.getItem('scanfile_max_height') === 'true'; } catch (e) { /* modo privado */ }
    if (saved) {
      document.body.classList.add('ultra-height-mode');
      update(true);
    }

    elements.btnTreeMaxHeight.addEventListener('click', function () {
      var isMax = document.body.classList.toggle('ultra-height-mode');
      try { localStorage.setItem('scanfile_max_height', String(isMax)); } catch (e) { /* modo privado */ }
      update(isMax);
      setTimeout(resizeTreemapCanvas, 60);
    });
  }

  /**
   * Remede o canvas em px de LAYOUT (clientWidth/clientHeight) e ajusta o
   * buffer pela escala visual do zoom. Assim o desenho não é escalado duas
   * vezes e o clique bate com o bloco desenhado (M11 / item 13).
   */
  function resizeTreemapCanvas() {
    if (!elements.treemapCanvas || !elements.treemapContainer) return;

    var layoutW = Math.floor(elements.treemapContainer.clientWidth);
    var layoutH = Math.floor(elements.treemapContainer.clientHeight);
    if (layoutW <= 0 || layoutH <= 0) return;

    var containerRect = elements.treemapContainer.getBoundingClientRect();
    var zoomScale = C.zoomScaleOf(containerRect, layoutW) || 1;
    var dpr = (window.devicePixelRatio || 1) * zoomScale;

    elements.treemapCanvas.style.width = layoutW + 'px';
    elements.treemapCanvas.style.height = layoutH + 'px';
    elements.treemapCanvas.width = Math.max(1, Math.round(layoutW * dpr));
    elements.treemapCanvas.height = Math.max(1, Math.round(layoutH * dpr));

    var ctx = elements.treemapCanvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    state.treemap.layoutWidth = layoutW;
    state.treemap.layoutHeight = layoutH;

    if (state.treemap.rawTree) {
      var maxDepth = state.treemap.depth || 5;
      var rootContainer;

      if (Array.isArray(state.treemap.rawTree)) {
        rootContainer = {
          name: 'Meus Discos',
          path: '',
          totalSize: state.treemap.rawTree.reduce(function (a, b) { return a + (b.totalSize || 0); }, 0),
          subDirs: state.treemap.rawTree,
          files: [],
          isFile: false,
        };
      } else {
        rootContainer = state.treemap.rawTree;
      }

      state.treemap.layoutNodes = computeSquarifiedLayout(rootContainer, 0, 0, layoutW, layoutH, 0, maxDepth);
    } else {
      state.treemap.layoutNodes = [];
    }

    renderTreemapCanvas();
  }

  function computeSquarifiedLayout(container, x, y, width, height, level, maxDepth, results) {
    results = results || [];
    if (width <= 0 || height <= 0 || !container || results.length >= 2000) return results;

    var effectiveMaxDepth = Math.min(maxDepth || 4, 8);
    var children = [];

    if (Array.isArray(container.subDirs)) {
      for (var i = 0; i < container.subDirs.length; i++) {
        var d = container.subDirs[i];
        if (d && (d.totalSize || 0) > 0) {
          children.push({
            name: d.name, path: d.path,
            totalSize: d.totalSize || 0,
            totalAllocatedSize: d.totalAllocatedSize || d.totalSize || 0,
            fileCount: d.fileCount || 0,
            subDirCount: d.subDirCount || 0,
            modTime: d.modTime, createTime: d.createTime,
            isFile: false, subDirs: d.subDirs, files: d.files,
          });
        }
      }
    }

    if (Array.isArray(container.files)) {
      for (var j = 0; j < container.files.length; j++) {
        var f = container.files[j];
        var sz = f ? (f.size || f.totalSize || 0) : 0;
        if (sz > 0) {
          children.push({
            name: f.name, path: f.path,
            totalSize: sz,
            totalAllocatedSize: f.allocatedSize || sz,
            fileCount: 1, subDirCount: 0,
            modTime: f.modTime, createTime: f.createTime,
            isFile: true,
          });
        }
      }
    }

    children.sort(function (a, b) { return b.totalSize - a.totalSize; });
    var totalChildSize = children.reduce(function (acc, c) { return acc + c.totalSize; }, 0);

    if (children.length === 0 || level >= effectiveMaxDepth || totalChildSize <= 0) {
      results.push({ x: x, y: y, w: width, h: height, node: container, level: level, isLeaf: true });
      return results;
    }

    var rects = C.squarify(children, totalChildSize, x, y, width, height);

    for (var k = 0; k < rects.length; k++) {
      var item = rects[k];
      var temFilhos = (item.node.subDirs && item.node.subDirs.length > 0) || (item.node.files && item.node.files.length > 0);
      var ehFolha = item.node.isFile || level + 1 >= effectiveMaxDepth || !temFilhos;

      if (ehFolha) {
        results.push({ x: item.x, y: item.y, w: item.w, h: item.h, node: item.node, level: level + 1, isLeaf: true });
      } else {
        computeSquarifiedLayout(item.node, item.x, item.y, item.w, item.h, level + 1, effectiveMaxDepth, results);
      }
    }

    return results;
  }

  function renderTreemapCanvas() {
    if (!elements.treemapCanvas) return;
    try {
      var ctx = elements.treemapCanvas.getContext('2d');
      var width = state.treemap.layoutWidth;
      var height = state.treemap.layoutHeight;

      ctx.clearRect(0, 0, width, height);

      if (!state.treemap.layoutNodes || state.treemap.layoutNodes.length === 0) {
        ctx.fillStyle = '#64748b';
        ctx.font = '14px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('Nenhum dado para exibir. Inicie uma Varredura.', width / 2, height / 2);
        ctx.textAlign = 'left';
        return;
      }

      var colorMode = state.treemap.colorMode || 'extension';
      var leaves = state.treemap.layoutNodes.filter(function (n) {
        return n.isLeaf && n.w > 0.5 && n.h > 0.5 && Number.isFinite(n.x) && Number.isFinite(n.y);
      });

      if (leaves.length > 1500) {
        leaves.sort(function (a, b) { return (b.w * b.h) - (a.w * a.h); });
        leaves = leaves.slice(0, 1500);
      }

      for (var i = 0; i < leaves.length; i++) {
        var n = leaves[i];
        var hovered = state.treemap.hoveredNode && state.treemap.hoveredNode.node && state.treemap.hoveredNode.node.path === n.node.path;
        var selected = state.treemap.selectedNode && state.treemap.selectedNode.node && state.treemap.selectedNode.node.path === n.node.path;
        drawCushionRect(ctx, n.x, n.y, n.w, n.h, getNodeColor(n.node, colorMode, n.level),
          hovered, selected, n.node.name, formatBytes(n.node.totalSize));
      }
    } catch (err) {
      console.warn('[Treemap] Erro ao renderizar:', err);
    }
  }

  function drawCushionRect(ctx, x, y, w, h, baseColor, isHovered, isSelected, label, sublabel) {
    if (w <= 0 || h <= 0) return;

    ctx.fillStyle = baseColor;
    ctx.fillRect(x, y, w, h);

    if (w >= 14 && h >= 14) {
      var cx = x + w * 0.35;
      var cy = y + h * 0.35;
      var radius = Math.max(w, h) * 0.9;

      var grad = ctx.createRadialGradient(cx, cy, 0, cx, cy, radius);
      grad.addColorStop(0, 'rgba(255, 255, 255, 0.45)');
      grad.addColorStop(0.35, 'rgba(255, 255, 255, 0.10)');
      grad.addColorStop(0.7, 'rgba(0, 0, 0, 0.25)');
      grad.addColorStop(1, 'rgba(0, 0, 0, 0.65)');

      ctx.fillStyle = grad;
      ctx.fillRect(x, y, w, h);

      if (w > 20 && h > 20) {
        ctx.fillStyle = 'rgba(255, 255, 255, 0.32)';
        ctx.fillRect(x, y, w, 1);
        ctx.fillRect(x, y, 1, h);
        ctx.fillStyle = 'rgba(0, 0, 0, 0.45)';
        ctx.fillRect(x, y + h - 1, w, 1);
        ctx.fillRect(x + w - 1, y, 1, h);
      }
    } else if (w >= 4 && h >= 4) {
      ctx.fillStyle = 'rgba(255, 255, 255, 0.18)';
      ctx.fillRect(x, y, w, 1);
      ctx.fillRect(x, y, 1, h);
      ctx.fillStyle = 'rgba(0, 0, 0, 0.35)';
      ctx.fillRect(x, y + h - 1, w, 1);
      ctx.fillRect(x + w - 1, y, 1, h);
    }

    if (isSelected) {
      ctx.strokeStyle = '#f59e0b';
      ctx.lineWidth = 2.5;
      ctx.strokeRect(x, y, w, h);
    } else if (isHovered) {
      ctx.strokeStyle = '#38bdf8';
      ctx.lineWidth = 2;
      ctx.strokeRect(x, y, w, h);
      ctx.fillStyle = 'rgba(56, 189, 248, 0.25)';
      ctx.fillRect(x, y, w, h);
    } else {
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.75)';
      ctx.lineWidth = 1;
      ctx.strokeRect(x, y, w, h);
    }

    if (w > 55 && h > 20 && label) {
      ctx.save();
      ctx.beginPath();
      ctx.rect(x + 2, y + 2, Math.max(0, w - 4), Math.max(0, h - 4));
      ctx.clip();

      ctx.fillStyle = '#ffffff';
      ctx.font = 'bold 11px Inter, sans-serif';
      ctx.shadowColor = 'rgba(0, 0, 0, 0.95)';
      ctx.shadowBlur = 4;
      ctx.shadowOffsetX = 1;
      ctx.shadowOffsetY = 1;
      ctx.fillText(label, x + 4, y + 13);

      if (h > 34 && sublabel) {
        ctx.fillStyle = 'rgba(255, 255, 255, 0.88)';
        ctx.font = '10px "JetBrains Mono", monospace';
        ctx.fillText(sublabel, x + 4, y + 26);
      }

      ctx.restore();
    }
  }

  function renderTreemapLegend() {
    if (!elements.treemapLegendBar) return;
    var colorMode = state.treemap.colorMode || 'extension';

    if (colorMode === 'depth') {
      elements.treemapLegendBar.innerHTML = LEVEL_COLORS.map(function (c, idx) {
        return '<div class="legend-chip"><span class="legend-color-dot" style="background:' + c + '"></span><span>Nível ' + idx + '</span></div>';
      }).join('');
      return;
    }

    if (colorMode === 'age') {
      var faixas = [
        { color: '#10b981', label: '< 1 Mês' },
        { color: '#06b6d4', label: '1 a 6 Meses' },
        { color: '#3b82f6', label: '6m a 1 Ano' },
        { color: '#eab308', label: '1 a 2 Anos' },
        { color: '#f97316', label: '2 a 5 Anos' },
        { color: '#ef4444', label: '> 5 Anos' },
      ];
      elements.treemapLegendBar.innerHTML = faixas.map(function (a) {
        return '<div class="legend-chip"><span class="legend-color-dot" style="background:' + a.color + '"></span><span>' + a.label + '</span></div>';
      }).join('');
      return;
    }

    var extStats = {};
    var totalTreeBytes = 0;
    var queue = [];

    if (Array.isArray(state.treemap.rawTree)) {
      state.treemap.rawTree.forEach(function (n) { if (n) queue.push(n); });
    } else if (state.treemap.rawTree) {
      queue.push(state.treemap.rawTree);
    }

    while (queue.length > 0) {
      var node = queue.pop();
      if (!node) continue;

      if (Array.isArray(node.files)) {
        for (var i = 0; i < node.files.length; i++) {
          var f = node.files[i];
          if (!f) continue;
          var sz = f.size || f.totalSize || 0;
          if (sz <= 0) continue;
          totalTreeBytes += sz;
          var ext = String(f.name || '').split('.').pop().toLowerCase();
          if (!extStats[ext]) extStats[ext] = { ext: ext, totalBytes: 0, count: 0, color: getExtensionColor(ext) };
          extStats[ext].totalBytes += sz;
          extStats[ext].count++;
        }
      }

      if (Array.isArray(node.subDirs)) {
        for (var j = 0; j < node.subDirs.length; j++) {
          if (node.subDirs[j]) queue.push(node.subDirs[j]);
        }
      }
    }

    var sorted = Object.keys(extStats).map(function (k) { return extStats[k]; })
      .sort(function (a, b) { return b.totalBytes - a.totalBytes; });

    if (sorted.length === 0) {
      elements.treemapLegendBar.innerHTML =
        '<div class="legend-chip"><span>Sem arquivos nesta pasta para compor a legenda.</span></div>';
      return;
    }

    elements.treemapLegendBar.innerHTML = sorted.slice(0, 10).map(function (t) {
      var pct = totalTreeBytes > 0 ? ((t.totalBytes / totalTreeBytes) * 100).toFixed(1) : '0.0';
      return '' +
        '<div class="legend-chip" title="' + esc(formatNumber(t.count)) + ' arquivos .' + esc(t.ext) + '">' +
          '<span class="legend-color-dot" style="background:' + esc(t.color) + '"></span>' +
          '<strong>.' + esc(t.ext) + '</strong>' +
          '<span class="legend-size">' + esc(formatBytes(t.totalBytes)) + ' (' + esc(pct) + '%)</span>' +
        '</div>';
    }).join('');
  }

  // ==========================================
  // DUPLICADOS
  // ==========================================

  async function loadDuplicates(page) {
    if (state.isLoadingDups) return;

    state.dupPage = Math.max(1, page || 1);
    var limit = state.dupLimit || 50;
    var offset = (state.dupPage - 1) * limit;

    var sortBy = elements.dupSortBy ? elements.dupSortBy.value : 'size_desc';
    var minSize = elements.dupMinSize ? elements.dupMinSize.value : '0';
    var search = elements.dupSearch ? elements.dupSearch.value.trim() : '';

    state.isLoadingDups = true;
    if (elements.duplicatesContainer) {
      elements.duplicatesContainer.textContent = 'Carregando duplicados por hash...';
    }

    try {
      var url = '/api/duplicates?sortBy=' + encodeURIComponent(sortBy) +
        '&minSize=' + encodeURIComponent(minSize) +
        '&search=' + encodeURIComponent(search) +
        '&limit=' + limit + '&offset=' + offset;

      var data = await apiGetJSON(url);
      state.duplicatesData = data || { groups: [] };
      renderDuplicates(state.duplicatesData);
    } catch (err) {
      if (elements.duplicatesContainer) {
        elements.duplicatesContainer.innerHTML = '<div class="empty-state">Erro: ' + esc(err.message) + '</div>';
      }
    } finally {
      state.isLoadingDups = false;
    }
  }

  function renderDuplicates(data) {
    if (!elements.duplicatesContainer) return;
    var groups = (data && data.groups) || [];

    if (groups.length === 0) {
      elements.duplicatesContainer.innerHTML =
        '<div class="empty-state">Nenhum arquivo duplicado encontrado com os filtros atuais.</div>';
      setText(elements.dupTotalGroups, '0');
      setText(elements.dupTotalFiles, '0');
      setText(elements.dupTotalWasted, '0 B');
      if (elements.dupPaginationBar) elements.dupPaginationBar.textContent = '';
      updateSelectionSummary();
      return;
    }

    setText(elements.dupTotalGroups, formatNumber(data.totalGroups || 0));
    setText(elements.dupTotalFiles, formatNumber(data.totalFiles || 0));
    setText(elements.dupTotalWasted, formatBytes(data.wastedBytes || 0));

    elements.duplicatesContainer.textContent = '';
    var fragment = document.createDocumentFragment();

    groups.forEach(function (grp) {
      var hash = String(grp.hash || '');
      var hashShort = hash.length > 22 ? hash.substring(0, 22) + '...' : (hash || 'hash');

      var card = document.createElement('div');
      card.className = 'dup-group-card';
      card.setAttribute('data-group-id', String(grp.id || ''));

      var linhas = (grp.files || []).map(function (file) {
        var marcado = state.selectedFilesForDelete.has(file.path);
        return '' +
          '<div class="dup-file-row ' + (marcado ? 'marked-for-delete' : '') + '">' +
            '<div class="dup-file-left">' +
              '<input type="checkbox" class="dup-file-checkbox" data-path="' + esc(file.path) + '" data-size="' + esc(file.size) + '" ' + (marcado ? 'checked' : '') + '>' +
              '<span class="dup-file-path truncate" title="' + esc(file.path) + '">' + esc(file.path) + '</span>' +
            '</div>' +
            '<div class="dup-file-date">' + esc(formatDate(file.modTime)) + '</div>' +
          '</div>';
      }).join('');

      card.innerHTML = '' +
        '<div class="dup-group-header">' +
          '<div class="dup-group-left">' +
            '<span class="dup-hash-pill" title="' + esc(hash) + '">' + esc(hashShort) + '</span>' +
            '<span class="dup-size-pill">' + esc(formatBytes(grp.fileSize)) + ' cada</span>' +
            '<span class="dup-wasted-badge">Desperdiçado: ' + esc(formatBytes(grp.wastedBytes)) + ' (' + esc(formatNumber(grp.fileCount)) + ' cópias)</span>' +
          '</div>' +
          '<div class="dup-group-actions">' +
            '<button class="btn btn-secondary btn-sm btn-group-keep-newest">⭐ Manter +Recente</button>' +
          '</div>' +
        '</div>' +
        '<div class="dup-file-list">' + linhas + '</div>';

      card.querySelectorAll('.dup-file-checkbox').forEach(function (cb) {
        cb.addEventListener('change', function () {
          var path = cb.getAttribute('data-path');
          var size = parseInt(cb.getAttribute('data-size'), 10) || 0;
          if (cb.checked) state.selectedFilesForDelete.set(path, size);
          else state.selectedFilesForDelete.delete(path);
          cb.closest('.dup-file-row').classList.toggle('marked-for-delete', cb.checked);
          updateSelectionSummary();
        });
      });

      var btnKeep = card.querySelector('.btn-group-keep-newest');
      if (btnKeep) {
        btnKeep.addEventListener('click', function () {
          var resultado = C.selectDuplicatesByStrategy([grp], 'keep_newest');
          resultado.kept.forEach(function (p) { state.selectedFilesForDelete.delete(p); });
          resultado.selected.forEach(function (f) { state.selectedFilesForDelete.set(f.path, f.size); });

          card.querySelectorAll('.dup-file-row').forEach(function (row) {
            var cb = row.querySelector('.dup-file-checkbox');
            if (!cb) return;
            var marcado = state.selectedFilesForDelete.has(cb.getAttribute('data-path'));
            cb.checked = marcado;
            row.classList.toggle('marked-for-delete', marcado);
          });
          updateSelectionSummary();
        });
      }

      fragment.appendChild(card);
    });

    elements.duplicatesContainer.appendChild(fragment);

    renderPaginationBar(
      elements.dupPaginationBar,
      data.totalGroups || 0,
      state.dupPage,
      state.dupLimit,
      function (newPage) { loadDuplicates(newPage); },
      function (newLimit) {
        state.dupLimit = newLimit;
        queueConfigChange({ dupLimit: newLimit });
        loadDuplicates(1);
      },
      'grupos'
    );

    updateSelectionSummary();
  }

  /**
   * Aplica a estratégia a TODOS os grupos carregados na página atual.
   * O núcleo garante que uma cópia de cada grupo sobrevive.
   */
  function selectDuplicatesByStrategy(strategy) {
    if (!state.duplicatesData || !state.duplicatesData.groups) return;

    state.selectedFilesForDelete.clear();
    var resultado = C.selectDuplicatesByStrategy(state.duplicatesData.groups, strategy);
    resultado.selected.forEach(function (f) { state.selectedFilesForDelete.set(f.path, f.size); });

    renderDuplicates(state.duplicatesData);
    showToast('Seleção aplicada: ' + (strategy === 'keep_newest' ? 'Manter +Recente' : 'Manter +Antigo') +
      '. Uma cópia de cada grupo foi preservada. A regra vale para os grupos desta página.', 'info');
  }

  function clearSelection() {
    state.selectedFilesForDelete.clear();
    if (state.duplicatesData) renderDuplicates(state.duplicatesData);
    updateSelectionSummary();
  }

  function updateSelectionSummary() {
    var count = state.selectedFilesForDelete.size;
    var totalBytes = 0;
    state.selectedFilesForDelete.forEach(function (sz) { totalBytes += sz; });

    setText(elements.dupSelectedCount, count + ' (' + formatBytes(totalBytes) + ')');

    if (elements.btnRecycleSelected) {
      elements.btnRecycleSelected.disabled = count === 0;
      elements.btnRecycleSelected.textContent = count === 0
        ? 'Mandar para Lixeira do Windows'
        : 'Mandar ' + count + ' para a Lixeira (' + formatBytes(totalBytes) + ')';
    }
    if (elements.btnDeleteSelectedDups) {
      elements.btnDeleteSelectedDups.disabled = count === 0;
    }
  }

  // ==========================================
  // RECICLAGEM E EXCLUSÃO PERMANENTE (contrato 1.5)
  // ==========================================

  function setupConfirmModals() {
    document.querySelectorAll('[data-close-modal]').forEach(function (btn) {
      btn.addEventListener('click', function () {
        closeModal(document.getElementById(btn.getAttribute('data-close-modal')));
      });
    });

    document.querySelectorAll('.modal-overlay').forEach(function (modal) {
      modal.addEventListener('click', function (e) {
        if (e.target === modal) closeModal(modal);
      });
    });

    if (elements.btnConfirmAction) {
      elements.btnConfirmAction.addEventListener('click', runConfirmedAction);
    }
    if (elements.confirmActionInput) {
      elements.confirmActionInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') {
          e.preventDefault();
          runConfirmedAction();
        }
      });
    }
  }

  function selectedEntries(origin) {
    var mapa = origin === 'idle' ? state.selectedIdleFiles : state.selectedFilesForDelete;
    var entradas = [];
    mapa.forEach(function (size, path) { entradas.push({ path: path, size: size }); });
    return entradas;
  }

  function requestRecycleFiles(origin) {
    var entradas = selectedEntries(origin);
    if (entradas.length === 0) return;

    openConfirmAction({
      kind: 'recycle',
      origin: origin,
      title: '🗑️ Enviar para a Lixeira do Windows',
      desc: 'Os itens abaixo vão para a Lixeira do Windows e podem ser restaurados de lá. ' +
        'Se algum volume não tiver Lixeira disponível, o servidor recusa o item em vez de apagá-lo.',
      entries: entradas,
      danger: false,
      requiresName: false,
    });
  }

  function requestDeleteFiles(origin) {
    var entradas = selectedEntries(origin);
    if (entradas.length === 0) return;

    openConfirmAction({
      kind: 'delete',
      origin: origin,
      title: '⛔ Excluir permanentemente',
      desc: 'Esta ação NÃO usa a Lixeira. Os arquivos são removidos de forma irreversível.',
      entries: entradas,
      danger: true,
      requiresDeleteWord: true,
    });
  }

  /** Reciclagem de um nó do treemap: pasta exige o nome digitado. */
  function requestRecycleNode(node) {
    if (!node || !node.path) return;
    var ehPasta = !node.isFile;

    openConfirmAction({
      kind: 'recycle',
      origin: 'tree',
      title: ehPasta ? '🗑️ Enviar pasta para a Lixeira' : '🗑️ Enviar arquivo para a Lixeira',
      desc: ehPasta
        ? 'Toda a pasta e o conteúdo dela vão para a Lixeira do Windows. Confirme digitando o nome da pasta.'
        : 'O arquivo vai para a Lixeira do Windows e pode ser restaurado de lá.',
      entries: [{ path: node.path, size: node.totalSize || 0 }],
      fileCount: ehPasta ? (node.fileCount || 0) : 0,
      danger: false,
      requiresName: ehPasta,
      folderName: ehPasta ? C.basename(node.path) : '',
    });
  }

  function openConfirmAction(action) {
    state.confirmAction = action;

    var totalBytes = action.entries.reduce(function (acc, e) { return acc + (Number(e.size) || 0); }, 0);

    setText(elements.confirmActionTitle, action.title);
    setText(elements.confirmActionDesc, action.desc);
    setText(elements.confirmActionCount, formatNumber(action.entries.length));
    setText(elements.confirmActionSize, formatBytes(totalBytes));

    if (elements.confirmActionFilesBox) {
      var mostraArquivos = !!action.fileCount;
      elements.confirmActionFilesBox.classList.toggle('hidden', !mostraArquivos);
      if (mostraArquivos) setText(elements.confirmActionFiles, formatNumber(action.fileCount));
    }

    if (elements.confirmActionList) {
      var amostra = action.entries.slice(0, 50);
      var extras = action.entries.length - amostra.length;
      elements.confirmActionList.innerHTML =
        amostra.map(function (e) {
          return '<div title="' + esc(e.path) + '">' + esc(e.path) + '</div>';
        }).join('') +
        (extras > 0 ? '<div class="confirm-more">... e mais ' + esc(formatNumber(extras)) + ' item(ns)</div>' : '');
    }

    if (elements.confirmActionInputGroup && elements.confirmActionInput) {
      var precisaTexto = !!(action.requiresName || action.requiresDeleteWord);
      elements.confirmActionInputGroup.classList.toggle('hidden', !precisaTexto);
      elements.confirmActionInput.value = '';

      if (action.requiresName) {
        setText(elements.confirmActionInputLabel, 'Digite o nome da pasta para confirmar:');
        setText(elements.confirmActionInputHint, 'Nome esperado: ' + action.folderName);
      } else if (action.requiresDeleteWord) {
        setText(elements.confirmActionInputLabel, 'Digite ' + C.DELETE_CONFIRM_WORD + ' para confirmar:');
        setText(elements.confirmActionInputHint, 'A exclusão permanente não tem volta.');
      }
    }

    if (elements.confirmActionError) {
      elements.confirmActionError.classList.add('hidden');
      elements.confirmActionError.textContent = '';
    }

    if (elements.btnConfirmAction) {
      elements.btnConfirmAction.textContent = action.kind === 'delete' ? 'Excluir permanentemente' : 'Enviar para a Lixeira';
      elements.btnConfirmAction.disabled = false;
    }

    openModal(elements.confirmActionModal);
    if (elements.confirmActionInput && (action.requiresName || action.requiresDeleteWord)) {
      setTimeout(function () { elements.confirmActionInput.focus(); }, 60);
    }
  }

  function showConfirmError(message) {
    if (!elements.confirmActionError) return;
    elements.confirmActionError.textContent = message;
    elements.confirmActionError.classList.remove('hidden');
  }

  async function runConfirmedAction() {
    var action = state.confirmAction;
    if (!action) return;

    var typed = elements.confirmActionInput ? elements.confirmActionInput.value : '';
    var body = { paths: action.entries.map(function (e) { return e.path; }) };
    var endpoint = '/api/files/recycle';

    if (action.kind === 'delete') {
      var v = C.validateDeleteConfirm(typed);
      if (!v.ok) {
        showConfirmError(v.reason);
        return;
      }
      body.confirmText = v.confirmText;
      endpoint = '/api/files/delete';
    } else if (action.requiresName) {
      var vn = C.validateFolderConfirm(action.folderName, typed);
      if (!vn.ok) {
        showConfirmError(vn.reason);
        return;
      }
      body.confirmName = vn.confirmName;
    } else {
      body.confirmName = '';
    }

    if (elements.btnConfirmAction) elements.btnConfirmAction.disabled = true;

    // Propostas do Assistente têm o próprio endpoint (confirm:true).
    if (typeof action.onConfirm === 'function') {
      try {
        var proprio = await action.onConfirm();
        if (!proprio || !proprio.ok) {
          showConfirmError((proprio && proprio.error) || 'Falha ao executar a Proposta.');
          if (elements.btnConfirmAction) elements.btnConfirmAction.disabled = false;
          return;
        }
        closeModal(elements.confirmActionModal);
        showActionResults(action, proprio.data);
        return;
      } catch (errProposta) {
        showConfirmError(errProposta.message);
        if (elements.btnConfirmAction) elements.btnConfirmAction.disabled = false;
        return;
      }
    }

    try {
      var res = await apiPostJSON(endpoint, body);
      if (!res.ok) {
        showConfirmError(res.error || 'Falha na operação.');
        if (elements.btnConfirmAction) elements.btnConfirmAction.disabled = false;
        return;
      }

      closeModal(elements.confirmActionModal);
      showActionResults(action, res.data);
      afterFileAction(action, res.data);
    } catch (err) {
      showConfirmError(err.message);
      if (elements.btnConfirmAction) elements.btnConfirmAction.disabled = false;
    }
  }

  function showActionResults(action, payload) {
    var resumo = C.summarizeItemResults(payload);

    setText(elements.actionResultsTitle, action.kind === 'delete'
      ? 'Resultado da Exclusão Permanente'
      : 'Resultado da Reciclagem');

    if (elements.actionResultsSummary) {
      elements.actionResultsSummary.innerHTML = '' +
        '<div class="dup-stat"><span class="dup-stat-title">Concluídos</span><span class="dup-stat-number">' + esc(formatNumber(resumo.okCount)) + '</span></div>' +
        '<div class="dup-stat"><span class="dup-stat-title">Recusados</span><span class="dup-stat-number">' + esc(formatNumber(resumo.counts.refused)) + '</span></div>' +
        '<div class="dup-stat"><span class="dup-stat-title">Falharam</span><span class="dup-stat-number">' + esc(formatNumber(resumo.counts.failed)) + '</span></div>' +
        '<div class="dup-stat highlight-info"><span class="dup-stat-title">Espaço liberado</span><span class="dup-stat-number">' + esc(formatBytes(resumo.freedBytes)) + '</span></div>';
    }

    if (elements.actionResultsTableBody) {
      if (resumo.items.length === 0) {
        elements.actionResultsTableBody.innerHTML =
          '<tr><td colspan="3" class="empty-state">O servidor não devolveu o detalhe por item.</td></tr>';
      } else {
        elements.actionResultsTableBody.innerHTML = resumo.items.map(function (it) {
          var status = String(it.status || 'failed');
          return '' +
            '<tr>' +
              '<td><span class="result-pill result-' + esc(status) + '">' + esc(STATUS_LABELS[status] || status) + '</span></td>' +
              '<td class="truncate" title="' + esc(it.path) + '">' + esc(it.path) + '</td>' +
              '<td>' + esc(it.reason || '-') + '</td>' +
            '</tr>';
        }).join('');
      }
    }

    openModal(elements.actionResultsModal);

    if (resumo.counts.refused > 0 || resumo.counts.failed > 0) {
      showToast(resumo.okCount + ' concluídos, ' + resumo.counts.refused + ' recusados, ' + resumo.counts.failed + ' falharam.', 'warning');
    } else {
      showToast(resumo.okCount + ' item(ns) processados. ' + formatBytes(resumo.freedBytes) + ' liberados.', 'success');
    }
  }

  var STATUS_LABELS = {
    recycled: 'Reciclado',
    deleted: 'Excluído',
    refused: 'Recusado',
    failed: 'Falhou',
  };

  function afterFileAction(action, payload) {
    var resumo = C.summarizeItemResults(payload);
    var concluidos = new Set();
    resumo.items.forEach(function (it) {
      if (it.status === 'recycled' || it.status === 'deleted') concluidos.add(it.path);
    });

    // Sem detalhe por item, tratamos a seleção inteira como consumida.
    var limparTudo = resumo.items.length === 0;

    if (action.origin === 'idle') {
      action.entries.forEach(function (e) {
        if (limparTudo || concluidos.has(e.path)) state.selectedIdleFiles.delete(e.path);
      });
      loadIdleFiles(state.idlePage);
    } else if (action.origin === 'duplicates') {
      action.entries.forEach(function (e) {
        if (limparTudo || concluidos.has(e.path)) state.selectedFilesForDelete.delete(e.path);
      });
      loadDuplicates(state.dupPage);
    } else if (action.origin === 'tree') {
      loadTreeData(state.treePath);
    }
  }

  // ==========================================
  // DISTRIBUIÇÃO POR EXTENSÃO
  // ==========================================

  async function loadAnalytics() {
    if (!elements.extensionsTableBody) return;
    try {
      var stats = await apiGetJSON('/api/stats/extensions');
      if (!Array.isArray(stats) || stats.length === 0) {
        elements.extensionsTableBody.innerHTML =
          '<tr><td colspan="5" class="empty-state">Sem dados para exibir.</td></tr>';
        return;
      }

      elements.extensionsTableBody.innerHTML = stats.map(function (st) {
        var pct = (st.percentage || 0).toFixed(1);
        return '' +
          '<tr>' +
            '<td><strong>' + esc(st.extension) + '</strong></td>' +
            '<td>' + esc(formatBytes(st.totalBytes)) + '</td>' +
            '<td>' + esc(formatNumber(st.fileCount)) + ' arquivos</td>' +
            '<td>' + esc(pct) + '%</td>' +
            '<td><div class="size-meter-cell"><div class="size-meter-bar"><div class="size-meter-fill" style="width: ' + pct + '%"></div></div></div></td>' +
          '</tr>';
      }).join('');
    } catch (err) {
      elements.extensionsTableBody.innerHTML =
        '<tr><td colspan="5" class="empty-state">' + esc(err.message) + '</td></tr>';
    }
  }

  // ==========================================
  // SNAPSHOTS
  // ==========================================

  function setupCacheModals() {
    if (elements.btnOpenSaveCacheModal) {
      elements.btnOpenSaveCacheModal.addEventListener('click', function () {
        var nowStr = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        if (elements.saveCacheFileName) elements.saveCacheFileName.value = 'scanfile_cache_' + nowStr + '.scanfile.gz';
        openModal(elements.saveCacheModal);
      });
    }

    if (elements.btnConfirmSaveCache) {
      elements.btnConfirmSaveCache.addEventListener('click', async function () {
        var fileName = elements.saveCacheFileName ? elements.saveCacheFileName.value.trim() : '';
        elements.btnConfirmSaveCache.disabled = true;
        elements.btnConfirmSaveCache.textContent = 'Salvando...';
        try {
          var res = await apiPostJSON('/api/cache/save', { fileName: fileName });
          if (!res.ok) throw new Error(res.error);
          showToast('Snapshot salvo em: ' + ((res.data && res.data.filePath) || fileName), 'success');
          closeModal(elements.saveCacheModal);
        } catch (err) {
          showToast('Erro ao salvar o Snapshot: ' + err.message, 'danger');
        } finally {
          elements.btnConfirmSaveCache.disabled = false;
          elements.btnConfirmSaveCache.textContent = 'Salvar Snapshot';
        }
      });
    }

    if (elements.btnOpenLoadCacheModal) {
      elements.btnOpenLoadCacheModal.addEventListener('click', function () {
        openModal(elements.loadCacheModal);
        fetchSavedCachesList();
      });
    }

    if (elements.btnLoadCustomCache) {
      elements.btnLoadCustomCache.addEventListener('click', function () {
        var path = elements.customCachePath ? elements.customCachePath.value.trim() : '';
        if (!path) {
          showToast('Informe o caminho do arquivo de Snapshot.', 'danger');
          return;
        }
        loadCacheFile(path);
      });
    }
  }

  async function fetchSavedCachesList() {
    if (!elements.savedCachesList) return;
    elements.savedCachesList.textContent = 'Buscando Snapshots salvos...';

    try {
      var list = await apiGetJSON('/api/cache/list');
      if (!Array.isArray(list) || list.length === 0) {
        elements.savedCachesList.innerHTML =
          '<div class="empty-state">Nenhum Snapshot salvo encontrado.</div>';
        return;
      }

      elements.savedCachesList.innerHTML = list.map(function (item) {
        var dateStr = item.modTime ? new Date(item.modTime).toLocaleString('pt-BR') : '-';
        return '' +
          '<div class="saved-cache-card">' +
            '<div class="saved-cache-info">' +
              '<div class="saved-cache-name">' + esc(item.fileName) + '</div>' +
              '<div class="saved-cache-meta">' +
                '<span>📅 ' + esc(dateStr) + '</span>' +
                '<span>📦 ' + esc(formatBytes(item.sizeBytes)) + '</span>' +
              '</div>' +
            '</div>' +
            '<button class="btn btn-primary btn-sm btn-load-cache-file" data-path="' + esc(item.filePath) + '">Carregar Snapshot</button>' +
          '</div>';
      }).join('');

      elements.savedCachesList.querySelectorAll('.btn-load-cache-file').forEach(function (btn) {
        btn.addEventListener('click', function () {
          loadCacheFile(btn.getAttribute('data-path'));
        });
      });
    } catch (err) {
      elements.savedCachesList.innerHTML = '<div class="empty-state">Erro: ' + esc(err.message) + '</div>';
    }
  }

  async function loadCacheFile(filePath) {
    setGlobalOperationLock(true, 'Carregando Snapshot para a memória...',
      'Descompactando o arquivo e reconstruindo a árvore e os índices...', 10, 'Lendo o arquivo do disco...');

    try {
      var res = await apiPostJSON('/api/cache/load', { filePath: filePath });
      if (!res.ok) throw new Error(res.error);

      var summary = (res.data && (res.data.summary || res.data.snapshot)) || {};
      closeModal(elements.loadCacheModal);
      updateGlobalProgress(100, 'Snapshot carregado.');
      showToast('Snapshot carregado: ' + formatNumber(summary.totalFiles) + ' arquivos (' + formatBytes(summary.totalBytes) + ').', 'success');
      applySnapshotSummary(summary);
    } catch (err) {
      showToast('Erro ao carregar o Snapshot: ' + err.message, 'danger');
    } finally {
      setTimeout(function () { setGlobalOperationLock(false); }, 350);
    }
  }

  // ==========================================
  // COMPARADOR DE PASTAS
  // ==========================================

  function setupFolderComparator() {
    document.querySelectorAll('.folder-subtab').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var target = btn.getAttribute('data-subtab');
        document.querySelectorAll('.folder-subtab').forEach(function (b) { b.classList.remove('active'); });
        document.querySelectorAll('.folder-subtab-pane').forEach(function (p) { p.classList.remove('active'); });

        btn.classList.add('active');
        var pane = document.getElementById(target);
        if (pane) pane.classList.add('active');
        state.currentFolderSubtab = target;

        if (target === 'folderDupSubtab') loadFolderDuplicates(state.folderDupPage);
      });
    });

    if (elements.btnSwapComparePaths) {
      elements.btnSwapComparePaths.addEventListener('click', function () {
        var tmp = elements.comparePathA.value;
        elements.comparePathA.value = elements.comparePathB.value;
        elements.comparePathB.value = tmp;
        queueConfigChange({
          comparePathA: elements.comparePathA.value.trim(),
          comparePathB: elements.comparePathB.value.trim(),
        });
      });
    }

    if (elements.btnRunFolderCompare) {
      elements.btnRunFolderCompare.addEventListener('click', runFolderCompare);
    }
  }

  async function loadFolderDuplicates(page) {
    if (!elements.folderDuplicatesContainer || state.isLoadingFolderDups) return;

    state.folderDupPage = Math.max(1, page || 1);
    var limit = state.folderDupLimit || 50;
    var offset = (state.folderDupPage - 1) * limit;

    var sortBy = elements.dupFolderSortBy ? elements.dupFolderSortBy.value : 'subdirs_desc';
    var minSize = elements.dupFolderMinSize ? elements.dupFolderMinSize.value : '0';
    var search = elements.dupFolderSearch ? elements.dupFolderSearch.value.trim() : '';
    var topLevelOnly = elements.chkFolderTopLevelOnly ? elements.chkFolderTopLevelOnly.checked : true;

    state.isLoadingFolderDups = true;
    elements.folderDuplicatesContainer.textContent = 'Identificando Pastas Clones...';

    try {
      var url = '/api/folders/duplicates?sortBy=' + encodeURIComponent(sortBy) +
        '&minSize=' + encodeURIComponent(minSize) +
        '&search=' + encodeURIComponent(search) +
        '&topLevelOnly=' + topLevelOnly +
        '&limit=' + limit + '&offset=' + offset;

      var data = await apiGetJSON(url);
      state.folderDuplicatesData = data || { groups: [] };
      renderFolderDuplicates(state.folderDuplicatesData);
    } catch (err) {
      elements.folderDuplicatesContainer.innerHTML = '<div class="empty-state">Erro: ' + esc(err.message) + '</div>';
    } finally {
      state.isLoadingFolderDups = false;
    }
  }

  function renderFolderDuplicates(data) {
    if (!elements.folderDuplicatesContainer) return;
    var groups = (data && data.groups) || [];

    if (groups.length === 0) {
      elements.folderDuplicatesContainer.innerHTML =
        '<div class="empty-state">Nenhuma Pasta Clone encontrada com os filtros atuais.</div>';
      setText(elements.dupFolderTotalGroups, '0');
      setText(elements.dupFolderTotalCount, '0');
      setText(elements.dupFolderTotalWasted, '0 B');
      if (elements.folderDupPaginationBar) elements.folderDupPaginationBar.textContent = '';
      return;
    }

    var apenasRaiz = elements.chkFolderTopLevelOnly && elements.chkFolderTopLevelOnly.checked;
    var totalTxt = apenasRaiz && data.topLevelGroups
      ? formatNumber(data.totalGroups) + ' (' + formatNumber(data.topLevelGroups) + ' Pastas Raiz)'
      : formatNumber(data.totalGroups || 0);

    setText(elements.dupFolderTotalGroups, totalTxt);
    setText(elements.dupFolderTotalCount, formatNumber(data.totalFolders || 0));
    setText(elements.dupFolderTotalWasted, formatBytes(data.wastedBytes || 0));

    elements.folderDuplicatesContainer.textContent = '';
    var fragment = document.createDocumentFragment();

    groups.forEach(function (grp) {
      var hash = String(grp.folderHash || '');
      var hashShort = hash.length > 24 ? hash.substring(0, 24) + '...' : (hash || 'hash');
      var isRoot = grp.isTopLevel !== false;

      // A confiança separa hash real de heurística por tamanho/data (M14).
      var confianca = String(grp.confidence || '');
      var selo = confianca === 'size_mtime'
        ? '<span class="dup-subfolder-badge" title="Sem hash completo: comparação por tamanho e data">≈ Semelhança por tamanho/data</span>'
        : (confianca === 'hash' ? '<span class="dup-root-badge" title="Comparação por hash de conteúdo">✅ Idêntica por hash</span>' : '');

      var levelBadge = isRoot
        ? '<span class="dup-root-badge">🌟 PASTA RAIZ CLONE</span>'
        : '<span class="dup-subfolder-badge">📁 Subpasta Contida</span>';

      var subdirsBadge = grp.subDirCount > 0
        ? '<span class="dup-subdirs-pill">🌳 ' + esc(formatNumber(grp.subDirCount)) + ' subpastas</span>'
        : '';

      var card = document.createElement('div');
      card.className = 'dup-group-card folder-dup-group-card ' + (isRoot ? 'is-root-clone' : '');

      var botaoComparar = (grp.folders && grp.folders.length >= 2)
        ? '<button class="btn btn-secondary btn-sm btn-quick-compare-folders" data-path-a="' + esc(grp.folders[0].path) + '" data-path-b="' + esc(grp.folders[1].path) + '">⚖️ Comparar Pasta 1 e 2</button>'
        : '';

      var linhas = (grp.folders || []).map(function (folder, idx) {
        var subs = folder.subDirCount > 0 ? ' &bull; 🌳 ' + esc(formatNumber(folder.subDirCount)) + ' subpastas' : '';
        return '' +
          '<div class="dup-file-row">' +
            '<div class="dup-file-left">' +
              '<span class="folder-badge-num">#' + (idx + 1) + '</span>' +
              '<span class="dup-file-path truncate" title="' + esc(folder.path) + '">' + esc(folder.path) + '</span>' +
            '</div>' +
            '<div class="dup-file-date">' +
              '<span>' + esc(formatNumber(folder.fileCount)) + ' arq' + subs + '</span> &bull; ' +
              '<strong>' + esc(formatBytes(folder.totalSize)) + '</strong>' +
            '</div>' +
          '</div>';
      }).join('');

      card.innerHTML = '' +
        '<div class="dup-group-header">' +
          '<div class="dup-group-left">' +
            levelBadge + selo +
            '<span class="dup-hash-pill" title="' + esc(hash) + '">📁 ' + esc(hashShort) + '</span>' +
            '<span class="dup-size-pill">' + esc(formatBytes(grp.folderSize)) + ' cada</span>' +
            '<span class="dup-size-pill">📄 ' + esc(formatNumber(grp.fileCount)) + ' arquivos</span>' +
            subdirsBadge +
            '<span class="dup-wasted-badge">Desperdício: ' + esc(formatBytes(grp.wastedBytes)) + ' (' + esc(formatNumber(grp.folderCount)) + ' cópias)</span>' +
          '</div>' +
          '<div class="dup-group-actions">' + botaoComparar + '</div>' +
        '</div>' +
        '<div class="dup-file-list">' + linhas + '</div>';

      card.querySelectorAll('.btn-quick-compare-folders').forEach(function (btn) {
        btn.addEventListener('click', function () {
          if (elements.comparePathA) elements.comparePathA.value = btn.getAttribute('data-path-a');
          if (elements.comparePathB) elements.comparePathB.value = btn.getAttribute('data-path-b');
          var compTabBtn = document.querySelector('.folder-subtab[data-subtab="folderCompareSubtab"]');
          if (compTabBtn) compTabBtn.click();
          runFolderCompare();
        });
      });

      fragment.appendChild(card);
    });

    elements.folderDuplicatesContainer.appendChild(fragment);

    renderPaginationBar(
      elements.folderDupPaginationBar,
      data.totalGroups || 0,
      state.folderDupPage,
      state.folderDupLimit,
      function (newPage) { loadFolderDuplicates(newPage); },
      function (newLimit) {
        state.folderDupLimit = newLimit;
        queueConfigChange({ folderDupLimit: newLimit });
        loadFolderDuplicates(1);
      },
      'grupos'
    );
  }

  // ==========================================
  // COMPARAÇÃO LADO A LADO
  // ==========================================

  var diffPage = 1;
  var diffLimit = 100;

  async function runFolderCompare() {
    if (!elements.comparePathA || !elements.comparePathB || !elements.folderCompareResults) return;

    var pathA = elements.comparePathA.value.trim();
    var pathB = elements.comparePathB.value.trim();

    if (!pathA || !pathB) {
      showToast('Informe o caminho da Pasta A e da Pasta B.', 'danger');
      return;
    }
    if (pathA.toLowerCase() === pathB.toLowerCase()) {
      showToast('A Pasta A e a Pasta B são o mesmo diretório.', 'danger');
      return;
    }

    elements.btnRunFolderCompare.disabled = true;
    elements.folderCompareResults.classList.remove('hidden');
    elements.folderCompareResults.textContent = 'Calculando árvores, hashes e diferenças...';

    try {
      var url = '/api/folders/compare?pathA=' + encodeURIComponent(pathA) + '&pathB=' + encodeURIComponent(pathB);
      var data = await apiGetJSON(url);
      state.folderComparisonData = data;
      state.folderDiffFilter = 'ALL';
      diffPage = 1;
      renderFolderComparison(data);
    } catch (err) {
      elements.folderCompareResults.innerHTML =
        '<div class="empty-state">Erro na comparação: ' + esc(err.message) + '</div>';
    } finally {
      elements.btnRunFolderCompare.disabled = false;
    }
  }

  function renderFolderComparison(data) {
    if (!data || !elements.folderCompareResults) return;

    // is100PercentMatch só vale com confiança de hash (M14).
    var confianca = String(data.confidence || 'hash');
    var isMatch = !!data.is100PercentMatch && confianca === 'hash';
    var matchPct = (data.matchPercentage || 0).toFixed(1);
    var entradas = Array.isArray(data.diffEntries) ? data.diffEntries : [];

    var avisoConfianca = confianca === 'size_mtime'
      ? '<div class="alert alert-warning">Comparação por tamanho e data de modificação: sem hash completo, a igualdade não é garantida.</div>'
      : '';

    var banner = isMatch
      ? '<div class="match-badge-big">✅ PASTAS IDÊNTICAS (mesmo conteúdo e mesmo hash)</div>' +
        '<p class="match-desc">Todos os arquivos têm o mesmo caminho relativo, o mesmo tamanho e o mesmo hash de conteúdo.</p>'
      : '<div class="match-badge-big">⚠️ PASTAS COM CONTEÚDO DIFERENTE (' + esc(matchPct) + '% de correspondência)</div>' +
        '<p class="match-desc">Divergências: ' + esc(formatNumber(data.modifiedCount)) + ' modificados, ' +
        esc(formatNumber(data.onlyInACount)) + ' exclusivos na Pasta A e ' +
        esc(formatNumber(data.onlyInBCount)) + ' exclusivos na Pasta B.</p>';

    elements.folderCompareResults.innerHTML = '' +
      '<div class="compare-match-banner ' + (isMatch ? 'match-identical' : 'match-different') + '">' + banner + '</div>' +
      avisoConfianca +
      '<div class="compare-overview-grid">' +
        folderCompareCard('Pasta A', data.pathA, data.totalSizeA, data.totalFilesA, data.folderHashA) +
        folderCompareCard('Pasta B', data.pathB, data.totalSizeB, data.totalFilesB, data.folderHashB) +
      '</div>' +
      '<div class="diff-filter-toolbar">' +
        '<div class="diff-filter-tabs">' +
          diffFilterButton('ALL', 'Todos', entradas.length) +
          diffFilterButton('IDENTICAL', '✅ Idênticos', data.identicalCount) +
          diffFilterButton('MODIFIED', '🔄 Modificados', data.modifiedCount) +
          diffFilterButton('ONLY_IN_A', '🅰️ Apenas em A', data.onlyInACount) +
          diffFilterButton('ONLY_IN_B', '🅱️ Apenas em B', data.onlyInBCount) +
        '</div>' +
        '<input type="text" id="diffSearchInput" class="form-input form-input-sm" placeholder="Filtrar por caminho...">' +
      '</div>' +
      '<div class="diff-table-wrapper">' +
        '<table class="diff-table">' +
          '<thead><tr>' +
            '<th style="width: 130px;">Situação</th>' +
            '<th>Caminho Relativo</th>' +
            '<th style="width: 140px;">Tamanho A</th>' +
            '<th style="width: 140px;">Tamanho B</th>' +
            '<th style="width: 220px;">Hash A / B</th>' +
          '</tr></thead>' +
          '<tbody id="folderDiffTableBody"></tbody>' +
        '</table>' +
      '</div>' +
      '<div id="folderDiffPaginationBar" class="pagination-bar"></div>';

    renderDiffEntriesTable();

    elements.folderCompareResults.querySelectorAll('.diff-filter-btn').forEach(function (btn) {
      btn.addEventListener('click', function () {
        state.folderDiffFilter = btn.getAttribute('data-filter');
        diffPage = 1;
        elements.folderCompareResults.querySelectorAll('.diff-filter-btn').forEach(function (b) { b.classList.remove('active'); });
        btn.classList.add('active');
        renderDiffEntriesTable();
      });
    });

    var diffSearch = document.getElementById('diffSearchInput');
    if (diffSearch) {
      diffSearch.addEventListener('input', debounce(function () {
        diffPage = 1;
        renderDiffEntriesTable();
      }, 250));
    }
  }

  function folderCompareCard(titulo, caminho, tamanho, arquivos, hash) {
    return '' +
      '<div class="compare-folder-card">' +
        '<div class="compare-card-title">' + esc(titulo) + '</div>' +
        '<div class="compare-folder-path truncate" title="' + esc(caminho) + '">' + esc(caminho) + '</div>' +
        '<div class="compare-metric-row"><span>Tamanho Total:</span><strong>' + esc(formatBytes(tamanho)) + '</strong></div>' +
        '<div class="compare-metric-row"><span>Total de Arquivos:</span><strong>' + esc(formatNumber(arquivos)) + ' arquivos</strong></div>' +
        '<div class="compare-metric-row"><span>Hash da pasta:</span><code class="hash-code truncate" title="' + esc(hash) + '">' + esc(hash) + '</code></div>' +
      '</div>';
  }

  function diffFilterButton(filtro, rotulo, contagem) {
    return '<button class="btn btn-secondary btn-sm diff-filter-btn ' + (state.folderDiffFilter === filtro ? 'active' : '') +
      '" data-filter="' + esc(filtro) + '">' + esc(rotulo) + ' (' + esc(formatNumber(contagem || 0)) + ')</button>';
  }

  var DIFF_STATUS = {
    IDENTICAL: ['status-OK', 'Idêntico'],
    MODIFIED: ['status-ERROR', 'Modificado'],
    ONLY_IN_A: ['status-LOCKED', 'Apenas em A'],
    ONLY_IN_B: ['status-READING', 'Apenas em B'],
  };

  function renderDiffEntriesTable() {
    var tbody = document.getElementById('folderDiffTableBody');
    if (!tbody || !state.folderComparisonData) return;

    var entries = state.folderComparisonData.diffEntries || [];
    var filter = state.folderDiffFilter || 'ALL';
    if (filter !== 'ALL') {
      entries = entries.filter(function (e) { return e.status === filter; });
    }

    var searchEl = document.getElementById('diffSearchInput');
    var search = searchEl ? searchEl.value.toLowerCase().trim() : '';
    if (search) {
      entries = entries.filter(function (e) {
        return String(e.relativePath || '').toLowerCase().indexOf(search) >= 0;
      });
    }

    if (entries.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Nenhum arquivo com os filtros selecionados.</td></tr>';
      var barraVazia = document.getElementById('folderDiffPaginationBar');
      if (barraVazia) barraVazia.textContent = '';
      return;
    }

    // Paginação da tabela de comparação (M12).
    var bounds = C.pageBounds(entries.length, diffPage, diffLimit);
    diffPage = bounds.page;
    var pagina = entries.slice(bounds.offset, bounds.offset + diffLimit);

    tbody.innerHTML = pagina.map(function (e) {
      var info = DIFF_STATUS[e.status] || ['status-OK', String(e.status || '-')];
      var sizeA = e.sizeA > 0 ? formatBytes(e.sizeA) : (e.status === 'ONLY_IN_B' ? '-' : '0 B');
      var sizeB = e.sizeB > 0 ? formatBytes(e.sizeB) : (e.status === 'ONLY_IN_A' ? '-' : '0 B');
      var hashA = e.hashA ? (e.hashA.length > 14 ? e.hashA.substring(0, 14) + '...' : e.hashA) : '-';
      var hashB = e.hashB ? (e.hashB.length > 14 ? e.hashB.substring(0, 14) + '...' : e.hashB) : '-';

      return '' +
        '<tr>' +
          '<td><span class="status-pill ' + esc(info[0]) + '">' + esc(info[1]) + '</span></td>' +
          '<td class="truncate" title="' + esc(e.relativePath) + '">' + esc(e.relativePath) + '</td>' +
          '<td><strong>' + esc(sizeA) + '</strong></td>' +
          '<td><strong>' + esc(sizeB) + '</strong></td>' +
          '<td><small class="diff-hash-a">A: ' + esc(hashA) + '</small><small class="diff-hash-b">B: ' + esc(hashB) + '</small></td>' +
        '</tr>';
    }).join('');

    renderPaginationBar(
      document.getElementById('folderDiffPaginationBar'),
      entries.length,
      diffPage,
      diffLimit,
      function (novaPagina) {
        diffPage = novaPagina;
        renderDiffEntriesTable();
      },
      function (novoLimite) {
        diffLimit = novoLimite;
        diffPage = 1;
        renderDiffEntriesTable();
      },
      'arquivos'
    );
  }

  // ==========================================
  // ARQUIVOS OCIOSOS
  // ==========================================

  async function loadIdleFiles(page) {
    if (!elements.idleTableBody) return;

    state.idlePage = Math.max(1, page || 1);
    var limit = state.idleLimit || 50;
    var offset = (state.idlePage - 1) * limit;

    var minAge = elements.idleMinAge ? elements.idleMinAge.value : '365';
    var minSize = elements.idleMinSize ? elements.idleMinSize.value : '104857600';
    var search = elements.idleSearch ? elements.idleSearch.value.trim() : '';
    var sortBy = elements.idleSortBy ? elements.idleSortBy.value : 'size_desc';

    elements.idleTableBody.innerHTML =
      '<tr><td colspan="6" class="loading-state">Analisando datas de modificação...</td></tr>';

    try {
      var url = '/api/stats/idle-files?minAgeDays=' + encodeURIComponent(minAge) +
        '&minSize=' + encodeURIComponent(minSize) +
        '&search=' + encodeURIComponent(search) +
        '&sortBy=' + encodeURIComponent(sortBy) +
        '&offset=' + offset + '&limit=' + limit;

      var data = await apiGetJSON(url);
      state.idleData = data;
      renderIdleFiles(data);
    } catch (err) {
      elements.idleTableBody.innerHTML =
        '<tr><td colspan="6" class="empty-state">Erro: ' + esc(err.message) + '</td></tr>';
    }
  }

  function idleFilesOf(data) {
    if (!data) return [];
    if (Array.isArray(data.topFiles)) return data.topFiles;
    if (Array.isArray(data.files)) return data.files;
    return [];
  }

  function renderIdleFiles(data) {
    if (!data || !elements.idleTableBody) return;

    setText(elements.idleTotalCount, formatNumber(data.totalIdleFiles || 0));
    setText(elements.idleTotalBytes, formatBytes(data.totalIdleBytes || 0));

    var buckets = data.buckets || data.ageBuckets || [];
    if (elements.idleBucketsGrid && buckets.length > 0) {
      elements.idleBucketsGrid.innerHTML = buckets.map(function (b) {
        return '' +
          '<div class="idle-bucket-card">' +
            '<div class="idle-bucket-label">' + esc(b.label) + '</div>' +
            '<div class="idle-bucket-size">' + esc(formatBytes(b.totalBytes)) + '</div>' +
            '<div class="idle-bucket-count">' + esc(formatNumber(b.fileCount)) + ' arquivos</div>' +
          '</div>';
      }).join('');
    }

    var files = idleFilesOf(data);
    if (files.length === 0) {
      elements.idleTableBody.innerHTML =
        '<tr><td colspan="6" class="empty-state">Nenhum Arquivo Ocioso com os filtros atuais.</td></tr>';
      if (elements.idlePaginationBar) elements.idlePaginationBar.textContent = '';
      updateIdleSelectionSummary();
      return;
    }

    elements.idleTableBody.innerHTML = files.map(function (file) {
      var marcado = state.selectedIdleFiles.has(file.path);
      var dias = file.daysInactive !== undefined ? file.daysInactive : (file.inactiveDays || 0);
      var anos = (dias / 365.25).toFixed(1);
      var rotulo = dias >= 365 ? anos + ' anos (' + formatNumber(dias) + ' dias)' : formatNumber(dias) + ' dias';

      return '' +
        '<tr class="' + (marcado ? 'marked-for-delete' : '') + '">' +
          '<td><input type="checkbox" class="idle-file-checkbox" data-path="' + esc(file.path) + '" data-size="' + esc(file.size) + '" ' + (marcado ? 'checked' : '') + '></td>' +
          '<td class="truncate" title="' + esc(file.path) + '">' +
            '<div class="tree-node-cell">' +
              '<span class="tree-icon file-icon">' +
                '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>' +
              '</span>' +
              '<span class="tree-node-name">' + esc(file.path) + '</span>' +
            '</div>' +
          '</td>' +
          '<td><strong>' + esc(formatBytes(file.size)) + '</strong></td>' +
          '<td><span class="idle-age-badge">' + esc(rotulo) + '</span></td>' +
          '<td>' + esc(formatDate(file.createTime)) + '</td>' +
          '<td>' + esc(formatDate(file.modTime)) + '</td>' +
        '</tr>';
    }).join('');

    elements.idleTableBody.querySelectorAll('.idle-file-checkbox').forEach(function (cb) {
      cb.addEventListener('change', function () {
        var path = cb.getAttribute('data-path');
        var size = parseInt(cb.getAttribute('data-size'), 10) || 0;
        if (cb.checked) state.selectedIdleFiles.set(path, size);
        else state.selectedIdleFiles.delete(path);
        cb.closest('tr').classList.toggle('marked-for-delete', cb.checked);
        updateIdleSelectionSummary();
      });
    });

    renderPaginationBar(
      elements.idlePaginationBar,
      data.totalIdleFiles || 0,
      state.idlePage,
      state.idleLimit,
      function (newPage) { loadIdleFiles(newPage); },
      function (newLimit) {
        state.idleLimit = newLimit;
        queueConfigChange({ idleLimit: newLimit });
        loadIdleFiles(1);
      },
      'arquivos'
    );

    updateIdleSelectionSummary();
  }

  function selectAllIdleFiles() {
    idleFilesOf(state.idleData).forEach(function (f) {
      state.selectedIdleFiles.set(f.path, f.size);
    });
    renderIdleFiles(state.idleData);
    if (elements.idleSelectAllCheckbox) elements.idleSelectAllCheckbox.checked = true;
    showToast('Selecionados os arquivos desta página.', 'info');
  }

  function clearIdleSelection() {
    state.selectedIdleFiles.clear();
    renderIdleFiles(state.idleData);
    if (elements.idleSelectAllCheckbox) elements.idleSelectAllCheckbox.checked = false;
  }

  function updateIdleSelectionSummary() {
    var count = state.selectedIdleFiles.size;
    var totalBytes = 0;
    state.selectedIdleFiles.forEach(function (sz) { totalBytes += sz; });

    setText(elements.idleSelectedCount, count + ' (' + formatBytes(totalBytes) + ')');

    if (elements.btnRecycleIdleSelected) {
      elements.btnRecycleIdleSelected.disabled = count === 0;
      elements.btnRecycleIdleSelected.textContent = count === 0
        ? 'Mandar para Lixeira do Windows'
        : 'Mandar ' + count + ' para a Lixeira (' + formatBytes(totalBytes) + ')';
    }
    if (elements.btnDeleteIdleSelected) {
      elements.btnDeleteIdleSelected.disabled = count === 0;
    }
  }

  // ==========================================
  // ASSISTENTE (contrato 1.11)
  // ==========================================

  function setupAIAssistant() {
    document.querySelectorAll('.btn-ai-provider').forEach(function (btn) {
      btn.addEventListener('click', function () {
        var provider = C.normalizeProvider(btn.getAttribute('data-provider'));
        state.ai.provider = provider;
        markActiveProviderButton();
        updateAIModelDropdown();
        queueConfigChange({ aiProvider: provider });
      });
    });

    if (elements.aiModelSelect) {
      elements.aiModelSelect.addEventListener('change', function () {
        state.ai.selectedModel = elements.aiModelSelect.value;
        checkIfModelNeedsPull();
        renderCatalogSidebar();
      });
    }

    if (elements.btnPullSelectedModel) {
      elements.btnPullSelectedModel.addEventListener('click', function () {
        if (state.ai.selectedModel) pullOllamaModel(state.ai.selectedModel);
      });
    }

    if (elements.btnOpenAIConfigModal && elements.aiConfigModal) {
      elements.btnOpenAIConfigModal.addEventListener('click', async function () {
        openModal(elements.aiConfigModal);
        try {
          var cfg = await apiGetJSON('/api/config');
          if (!cfg) return;
          state.serverConfig = state.serverConfig ? Object.assign(state.serverConfig, cfg) : cfg;
          state.hasOpenRouterKey = !!cfg.hasOpenRouterKey;

          if (elements.aiOllamaEndpointInput) {
            elements.aiOllamaEndpointInput.value = cfg.aiOllamaEndpoint || 'http://127.0.0.1:11434';
          }
          // O GET nunca traz a chave: o campo fica vazio de propósito.
          if (elements.aiOpenRouterKeyInput) elements.aiOpenRouterKeyInput.value = '';
          if (elements.aiDryRunDefaultCheckbox) elements.aiDryRunDefaultCheckbox.checked = cfg.aiDryRunDefault !== false;
          updateOpenRouterKeyHint();
        } catch (e) {
          console.warn('Falha ao ler a Configuração de IA:', e);
        }
      });
    }

    if (elements.btnSaveAIConfig) {
      elements.btnSaveAIConfig.addEventListener('click', function () {
        var patch = {};

        if (elements.aiOllamaEndpointInput) {
          patch.aiOllamaEndpoint = elements.aiOllamaEndpointInput.value.trim() || 'http://127.0.0.1:11434';
        }
        if (elements.aiDryRunDefaultCheckbox) {
          patch.aiDryRunDefault = elements.aiDryRunDefaultCheckbox.checked;
        }

        // A chave só sobe quando o usuário realmente digitou algo (1.6).
        var digitada = elements.aiOpenRouterKeyInput ? elements.aiOpenRouterKeyInput.value.trim() : '';
        if (digitada) patch.aiOpenRouterKey = digitada;

        queueConfigChange(patch);
        if (elements.aiOpenRouterKeyInput) elements.aiOpenRouterKeyInput.value = '';
        showToast('Configurações do Assistente salvas.', 'success');
        closeModal(elements.aiConfigModal);
        loadAICatalog();
      });
    }

    if (elements.btnClearOpenRouterKey) {
      elements.btnClearOpenRouterKey.addEventListener('click', function () {
        // Uma chave vazia explícita remove o segredo guardado (1.6).
        queueConfigChange({ aiOpenRouterKey: '' });
        state.hasOpenRouterKey = false;
        updateOpenRouterKeyHint();
        showToast('Chave do OpenRouter removida.', 'info');
      });
    }

    if (elements.btnClearAIChat) {
      elements.btnClearAIChat.addEventListener('click', function () {
        state.ai.chatHistory = [];
        if (!elements.aiMessagesContainer) return;
        elements.aiMessagesContainer.innerHTML = '' +
          '<div class="ai-welcome-message">' +
            '<div class="ai-avatar">🤖</div>' +
            '<div class="ai-bubble">' +
              '<h4>Conversa reiniciada.</h4>' +
              '<p>Toda Proposta continua nascendo pendente e só é executada com a sua aprovação.</p>' +
              '<div class="ai-quick-prompts">' +
                '<button class="ai-prompt-chip" data-prompt="Ache todas as duplicatas maiores que 50MB e gere uma proposta de limpeza">🔍 Achar duplicatas &gt; 50MB</button>' +
                '<button class="ai-prompt-chip" data-prompt="Classifique os maiores arquivos deste disco por tipo e resuma o espaço">📊 Classificar maiores arquivos</button>' +
                '<button class="ai-prompt-chip" data-prompt="Quais são os arquivos ociosos há mais de 1 ano que estão ocupando mais espaço?">⏳ Arquivos ociosos &gt; 1 ano</button>' +
              '</div>' +
            '</div>' +
          '</div>';
        attachQuickPromptListeners();
      });
    }

    if (elements.btnSendAIMessage) elements.btnSendAIMessage.addEventListener('click', function () { sendAIMessage(); });

    if (elements.aiPromptInput) {
      elements.aiPromptInput.addEventListener('keydown', function (e) {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          sendAIMessage();
        }
      });
    }

    markActiveProviderButton();
    attachQuickPromptListeners();
  }

  function markActiveProviderButton() {
    document.querySelectorAll('.btn-ai-provider').forEach(function (b) {
      var p = C.normalizeProvider(b.getAttribute('data-provider'));
      b.classList.toggle('active', p === state.ai.provider);
    });
  }

  function updateOpenRouterKeyHint() {
    if (elements.aiOpenRouterKeyHint) {
      elements.aiOpenRouterKeyHint.textContent = state.hasOpenRouterKey
        ? '🔑 Chave configurada. Deixe em branco para mantê-la; digite outra para substituir.'
        : 'Nenhuma chave salva. A chave é guardada protegida e nunca devolvida pela API.';
    }
    if (elements.btnClearOpenRouterKey) {
      elements.btnClearOpenRouterKey.classList.toggle('hidden', !state.hasOpenRouterKey);
    }
  }

  function attachQuickPromptListeners() {
    document.querySelectorAll('.ai-prompt-chip').forEach(function (chip) {
      chip.addEventListener('click', function () {
        if (!elements.aiPromptInput) return;
        elements.aiPromptInput.value = chip.getAttribute('data-prompt') || '';
        sendAIMessage();
      });
    });
  }

  async function loadAICatalog() {
    try {
      var payload = await apiGetJSON('/api/ai/models');
      var normalizado = C.normalizeModelCatalog(payload);
      state.ai.models = normalizado.models;
      state.ai.ollamaOnline = normalizado.ollamaOnline;

      if (elements.ollamaStatusDot) {
        elements.ollamaStatusDot.className = 'provider-dot ' + (normalizado.ollamaOnline ? 'online' : '');
        elements.ollamaStatusDot.title = normalizado.ollamaOnline ? 'Ollama disponível' : 'Ollama indisponível';
      }
      if (elements.openrouterStatusDot) {
        elements.openrouterStatusDot.className = 'provider-dot ' + (state.hasOpenRouterKey ? 'online' : '');
        elements.openrouterStatusDot.title = state.hasOpenRouterKey ? 'Chave configurada' : 'Sem chave de API';
      }

      updateAIModelDropdown();
      renderCatalogSidebar();
    } catch (err) {
      console.warn('Erro ao carregar o catálogo do Assistente:', err);
      if (elements.aiModelsList) {
        elements.aiModelsList.innerHTML = '<div class="empty-state">Não foi possível carregar o catálogo: ' + esc(err.message) + '</div>';
      }
    }
  }

  function modelsForCurrentProvider() {
    if (state.ai.provider === 'quick') return [];
    return state.ai.models.filter(function (m) {
      return m.provider === state.ai.provider;
    });
  }

  function updateAIModelDropdown() {
    if (!elements.aiModelSelect) return;

    if (state.ai.provider === 'quick') {
      elements.aiModelSelect.innerHTML = '<option value="">Comandos Rápidos não usam modelo</option>';
      elements.aiModelSelect.disabled = true;
      state.ai.selectedModel = '';
      checkIfModelNeedsPull();
      return;
    }

    elements.aiModelSelect.disabled = false;
    var lista = modelsForCurrentProvider();

    if (lista.length === 0) {
      elements.aiModelSelect.innerHTML = '<option value="">Nenhum modelo disponível para este provedor</option>';
      state.ai.selectedModel = '';
      checkIfModelNeedsPull();
      return;
    }

    elements.aiModelSelect.textContent = '';
    lista.forEach(function (m) {
      var opt = document.createElement('option');
      opt.value = m.id;
      opt.textContent = m.name +
        (m.sizeGB ? ' (' + m.sizeGB.toFixed(1) + ' GB)' : '') +
        (m.installed ? ' [Instalado]' : '') +
        (m.vision ? ' 👁️' : '') +
        (m.tools ? ' 🛠️' : '');
      elements.aiModelSelect.appendChild(opt);
    });

    var existe = lista.some(function (m) { return m.id === state.ai.selectedModel; });
    if (!existe) {
      var recomendado = lista.find(function (m) { return m.recommended; });
      state.ai.selectedModel = (recomendado || lista[0]).id;
    }
    elements.aiModelSelect.value = state.ai.selectedModel;

    checkIfModelNeedsPull();
  }

  function checkIfModelNeedsPull() {
    if (!elements.btnPullSelectedModel) return;

    if (state.ai.provider !== 'ollama' || !state.ai.selectedModel) {
      elements.btnPullSelectedModel.classList.add('hidden');
      return;
    }

    var item = state.ai.models.find(function (m) { return m.id === state.ai.selectedModel; });
    if (item && !item.installed && state.ai.ollamaOnline) {
      elements.btnPullSelectedModel.classList.remove('hidden');
      elements.btnPullSelectedModel.textContent = '⬇️ Baixar ' + item.id + (item.sizeGB ? ' (' + item.sizeGB.toFixed(1) + ' GB)' : '');
    } else {
      elements.btnPullSelectedModel.classList.add('hidden');
    }
  }

  function renderCatalogSidebar() {
    if (!elements.aiModelsList) return;

    var models = state.ai.models;
    if (!models || models.length === 0) {
      elements.aiModelsList.innerHTML = '<div class="empty-state">Nenhum modelo no catálogo.</div>';
      return;
    }

    elements.aiModelsList.innerHTML = models.map(function (m) {
      var selecionado = m.id === state.ai.selectedModel;

      var selos = '' +
        (m.vision
          ? '<span class="model-flag flag-vision" title="Lê imagens e faz OCR">👁️ Visão</span>'
          : '<span class="model-flag flag-none" title="Só texto">Sem visão</span>') +
        (m.tools
          ? '<span class="model-flag flag-tools" title="Chama as ferramentas MCP">🛠️ Ferramentas</span>'
          : '<span class="model-flag flag-none" title="Não chama ferramentas">Sem ferramentas</span>') +
        (m.fitsMemory ? '' : '<span class="model-flag flag-memory" title="Maior que a memória disponível">⚠️ Não cabe na RAM</span>');

      var status = m.installed
        ? '<span class="model-status-tag">✅ Pronto para uso</span>'
        : '<span class="model-status-tag not-installed">☁️ Download necessário</span>';

      var avisoVisao = m.vision ? '' :
        '<p class="model-vision-warning">Sem visão: não lê imagens nem PDFs escaneados; use um modelo com o selo Visão para isso.</p>';

      var recomendado = m.recommended
        ? '<div class="model-recommended-tag">⭐ RECOMENDADO</div>'
        : '';

      return '' +
        '<div class="model-catalog-item ' + (selecionado ? 'active-model' : '') + ' ' + (m.recommended ? 'primary-model-card' : '') + '">' +
          recomendado +
          '<div class="model-item-header">' +
            '<span class="model-item-name">' + esc(m.name) + '</span>' +
            '<div class="model-badges">' + selos + '</div>' +
          '</div>' +
          '<div class="model-item-specs">' +
            '<span>💾 ' + esc(m.sizeGB ? m.sizeGB.toFixed(1) + ' GB' : 'tamanho desconhecido') + '</span>' +
            '<span>' + esc(C.providerLabel(m.provider)) + '</span>' +
          '</div>' +
          avisoVisao +
          '<div class="model-item-footer">' +
            status +
            '<button class="btn btn-secondary btn-sm btn-select-catalog-model" data-model-id="' + esc(m.id) + '">' +
              (m.installed ? 'Selecionar' : '⬇️ Baixar') +
            '</button>' +
          '</div>' +
        '</div>';
    }).join('');

    elements.aiModelsList.querySelectorAll('.btn-select-catalog-model').forEach(function (btn) {
      btn.addEventListener('click', function (e) {
        e.stopPropagation();
        var modelId = btn.getAttribute('data-model-id');
        var item = state.ai.models.find(function (m) { return m.id === modelId; });
        if (!item) return;

        state.ai.provider = item.provider;
        markActiveProviderButton();
        state.ai.selectedModel = modelId;
        updateAIModelDropdown();

        if (!item.installed && item.provider === 'ollama') {
          pullOllamaModel(modelId);
        } else {
          showToast('Modelo ' + modelId + ' selecionado.', 'info');
          renderCatalogSidebar();
        }
      });
    });
  }

  async function pullOllamaModel(modelName) {
    if (state.ai.isPulling) return;
    state.ai.isPulling = true;

    if (elements.aiPullProgressContainer) {
      elements.aiPullProgressContainer.classList.remove('hidden');
      setText(elements.pullModelTitle, 'Baixando o modelo ' + modelName + '...');
      setText(elements.pullModelPercent, '0%');
      if (elements.pullProgressBar) elements.pullProgressBar.style.width = '0%';
      setText(elements.pullModelStatus, 'Iniciando o download...');
    }

    try {
      var response = await apiFetch('/api/ai/models/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: modelName }),
      });
      if (!response.ok) throw new Error(await readError(response));

      await consumeSSEStream(response, function (data) {
        if (data.status) setText(elements.pullModelStatus, data.status);
        if (data.percent !== undefined) {
          var p = Math.round(data.percent);
          setText(elements.pullModelPercent, p + '%');
          if (elements.pullProgressBar) elements.pullProgressBar.style.width = p + '%';
        }
      });

      showToast('Modelo ' + modelName + ' instalado.', 'success');
      await loadAICatalog();
    } catch (err) {
      showToast('Falha no download do modelo: ' + err.message, 'danger');
    } finally {
      state.ai.isPulling = false;
      setTimeout(function () {
        if (elements.aiPullProgressContainer) elements.aiPullProgressContainer.classList.add('hidden');
      }, 3000);
    }
  }

  /** Lê um corpo `text/event-stream` linha a linha, com JSON.parse protegido. */
  async function consumeSSEStream(response, onEvent) {
    var reader = response.body.getReader();
    var decoder = new TextDecoder('utf-8');
    var buffer = '';

    while (true) {
      var chunk = await reader.read();
      if (chunk.done) break;

      buffer += decoder.decode(chunk.value, { stream: true });
      var lines = buffer.split('\n');
      buffer = lines.pop() || '';

      for (var i = 0; i < lines.length; i++) {
        var line = lines[i];
        if (line.indexOf('data: ') !== 0) continue;
        var jsonStr = line.slice(6).trim();
        if (!jsonStr) continue;
        var data = C.safeParseJSON(jsonStr, null);
        if (data) onEvent(data);
      }
    }
  }

  async function sendAIMessage() {
    if (!elements.aiPromptInput || !elements.aiMessagesContainer) return;

    var prompt = elements.aiPromptInput.value.trim();
    if (!prompt || state.ai.isGenerating) return;

    state.ai.isGenerating = true;
    elements.aiPromptInput.value = '';
    if (elements.btnSendAIMessage) elements.btnSendAIMessage.disabled = true;

    appendChatMessage('user', prompt);

    var msgId = 'msg_' + Date.now();
    var assistantMsg = document.createElement('div');
    assistantMsg.className = 'chat-msg assistant';
    assistantMsg.id = msgId;
    assistantMsg.innerHTML = '' +
      '<div class="ai-avatar">🤖</div>' +
      '<div class="msg-bubble">' +
        '<div class="tool-status-container"></div>' +
        '<div class="thought-container"><span class="tool-spinner"></span> <em>Consultando as ferramentas...</em></div>' +
        '<div class="content-container"></div>' +
        '<div class="proposal-container"></div>' +
      '</div>';
    elements.aiMessagesContainer.appendChild(assistantMsg);
    scrollChatToBottom();

    var toolsEl = assistantMsg.querySelector('.tool-status-container');
    var thoughtEl = assistantMsg.querySelector('.thought-container');
    var contentEl = assistantMsg.querySelector('.content-container');
    var proposalsEl = assistantMsg.querySelector('.proposal-container');
    var toolBadges = {};
    var accumulated = '';

    try {
      var payload = {
        provider: state.ai.provider,
        model: state.ai.selectedModel,
        prompt: prompt,
        history: state.ai.chatHistory.slice(-8),
        selectedFolder: state.treePath,
      };

      var response = await apiFetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
      if (!response.ok) throw new Error(await readError(response));

      await consumeSSEStream(response, function (ev) {
        if (ev.type === 'thought') {
          thoughtEl.innerHTML = '<span class="tool-spinner"></span> <em>' + esc(ev.content) + '</em>';
        } else if (ev.type === 'tool_start') {
          thoughtEl.textContent = '';
          var nome = ev.toolName || 'ferramenta';
          var badge = document.createElement('div');
          badge.className = 'tool-status-badge';
          badge.innerHTML = '<span class="tool-spinner"></span> Executando <strong>' + esc(nome) + '</strong>...';
          toolBadges[nome] = badge;
          toolsEl.appendChild(badge);
        } else if (ev.type === 'tool_end') {
          var nomeFim = ev.toolName || 'ferramenta';
          var existente = toolBadges[nomeFim];
          if (existente) {
            existente.innerHTML = '✅ Ferramenta <strong>' + esc(nomeFim) + '</strong> concluída.';
          }
        } else if (ev.type === 'token') {
          thoughtEl.textContent = '';
          accumulated += ev.content || '';
          contentEl.innerHTML = C.renderMarkdown(accumulated);
        } else if (ev.type === 'proposal' && ev.proposal) {
          renderActionProposalCard(ev.proposal, proposalsEl);
        } else if (ev.type === 'error') {
          contentEl.innerHTML += '<div class="alert alert-danger">❌ ' + esc(ev.content) + '</div>';
        }
        scrollChatToBottom();
      });

      if (!accumulated) thoughtEl.textContent = '';

      state.ai.chatHistory.push({ role: 'user', content: prompt });
      state.ai.chatHistory.push({ role: 'assistant', content: accumulated });
    } catch (err) {
      contentEl.innerHTML = '<div class="alert alert-danger">Erro na execução do Assistente: ' + esc(err.message) + '</div>';
    } finally {
      state.ai.isGenerating = false;
      if (elements.btnSendAIMessage) elements.btnSendAIMessage.disabled = false;
    }
  }

  function scrollChatToBottom() {
    if (elements.aiMessagesContainer) {
      elements.aiMessagesContainer.scrollTop = elements.aiMessagesContainer.scrollHeight;
    }
  }

  function appendChatMessage(role, text) {
    if (!elements.aiMessagesContainer) return;
    var msg = document.createElement('div');
    msg.className = 'chat-msg ' + role;
    msg.innerHTML = '' +
      '<div class="ai-avatar">' + (role === 'user' ? '👤' : '🤖') + '</div>' +
      '<div class="msg-bubble">' + C.renderMarkdown(text) + '</div>';
    elements.aiMessagesContainer.appendChild(msg);
    scrollChatToBottom();
  }

  /** Proposta: sempre pendente; a execução exige confirm:true (contrato 1.11). */
  function renderActionProposalCard(proposal, containerEl) {
    if (!containerEl || !proposal) return;

    var card = document.createElement('div');
    card.className = 'ai-proposal-card';

    var arquivos = Array.isArray(proposal.files) ? proposal.files : [];
    var amostra = arquivos.slice(0, 10).map(function (f) {
      return '<div class="proposal-file-item" title="' + esc(f) + '">📄 ' + esc(f) + '</div>';
    }).join('');
    var restantes = (proposal.fileCount || arquivos.length) - Math.min(10, arquivos.length);

    card.innerHTML = '' +
      '<div class="proposal-header">' +
        '<span class="proposal-type-badge">⚡ PROPOSTA: ' + esc(proposal.actionType) + '</span>' +
        '<span class="proposal-stats">Afeta ' + esc(formatNumber(proposal.fileCount)) + ' arquivos (' + esc(proposal.totalSize) + ')</span>' +
      '</div>' +
      '<p class="proposal-pending-note">Pendente: nada foi executado. A ação só acontece se você aprovar aqui.</p>' +
      '<p class="proposal-desc">' + esc(proposal.description || '') + '</p>' +
      '<div class="proposal-files-preview">' + amostra +
        (restantes > 0 ? '<div class="proposal-more">... e mais ' + esc(formatNumber(restantes)) + ' arquivo(s)</div>' : '') +
      '</div>' +
      '<div class="proposal-actions">' +
        '<button class="btn btn-secondary btn-sm btn-proposal-reject">✖ Descartar Proposta</button>' +
        '<button class="btn btn-danger btn-sm btn-proposal-execute">✅ Aprovar e executar</button>' +
      '</div>';

    containerEl.appendChild(card);

    card.querySelector('.btn-proposal-reject').addEventListener('click', function () {
      card.innerHTML = '<div class="proposal-discarded">Proposta descartada. Nada foi alterado no disco.</div>';
    });

    card.querySelector('.btn-proposal-execute').addEventListener('click', function () {
      openConfirmAction({
        kind: proposal.actionType === 'DELETE' ? 'delete' : 'recycle',
        origin: 'proposal',
        title: 'Aprovar a Proposta do Assistente',
        desc: 'Ação ' + proposal.actionType + ' sobre ' + formatNumber(proposal.fileCount) + ' arquivo(s).',
        entries: arquivos.map(function (f) { return { path: f, size: 0 }; }),
        danger: proposal.actionType === 'DELETE',
        requiresDeleteWord: proposal.actionType === 'DELETE',
        proposalId: proposal.proposalId,
        onConfirm: async function () {
          var res = await apiPostJSON('/api/ai/actions/execute', {
            proposalId: proposal.proposalId,
            confirm: true,
          });
          if (!res.ok) return { ok: false, error: res.error };
          card.innerHTML = '<div class="proposal-executed">✅ Proposta executada. ' + esc((res.data && res.data.message) || '') + '</div>';
          return { ok: true, data: res.data };
        },
      });
    });
  }

  // ==========================================
  // ARRANQUE
  // ==========================================

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
