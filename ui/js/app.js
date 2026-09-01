// ScanFile Pro - Interactive Frontend Application
(function () {
  'use strict';

  // State
  const state = {
    drives: [],
    selectedRoots: new Set(),
    isScanning: false,
    currentTab: 'drivesTab',
    currentFolderSubtab: 'folderDupSubtab',
    treePath: '',
    treeData: null,
    expandedDirs: new Set(),
    treemap: {
      depth: 5,
      colorMode: 'extension', // 'extension' | 'depth' | 'age'
      viewMode: 'split',      // 'split' | 'treemap' | 'table'
      rawTree: null,
      layoutNodes: [],
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
    dupOffset: 0,
    dupGroups: [],
    dupTotalGroups: 0,
    folderDupPage: 1,
    folderDupLimit: 50,
    folderDupOffset: 0,
    folderDupGroups: [],
    folderDupTotalGroups: 0,
    idlePage: 1,
    idleLimit: 50,
    idleSortBy: 'size_desc',
    treeTablePage: 1,
    treeTableLimit: 50,
    isLoadingDups: false,
    isLoadingFolderDups: false,
    selectedFilesForDelete: new Map(), // Map<path, size>
    selectedIdleFiles: new Map(),      // Map<path, size>
    idleData: null,
    eventLogs: [],
    sseSource: null,
    uiZoom: 100,
    theme: 'ochre-dark',
    ai: {
      provider: 'ollama',
      selectedModel: 'qwen2.5:1.5b',
      modelsCatalog: null,
      chatHistory: [],
      isGenerating: false,
      isPulling: false,
    },
  };

  // DOM Elements
  const elements = {
    themeSelector: document.getElementById('themeSelector'),
    memAppUsage: document.getElementById('memAppUsage'),
    memSysUsage: document.getElementById('memSysUsage'),
    memoryBarFill: document.getElementById('memoryBarFill'),
    footerStatusText: document.getElementById('footerStatusText'),
    footerMemoryText: document.getElementById('footerMemoryText'),
    footerThreadsText: document.getElementById('footerThreadsText'),
    footerRootsCount: document.getElementById('footerRootsCount'),
    treePaginationBar: document.getElementById('treePaginationBar'),
    dupPaginationBar: document.getElementById('dupPaginationBar'),
    folderDupPaginationBar: document.getElementById('folderDupPaginationBar'),
    idlePaginationBar: document.getElementById('idlePaginationBar'),
    idleSortBy: document.getElementById('idleSortBy'),
    btnZoomIn: document.getElementById('btnZoomIn'),
    btnZoomOut: document.getElementById('btnZoomOut'),
    zoomLevelDisplay: document.getElementById('zoomLevelDisplay'),
    privilegeBadge: document.getElementById('privilegeBadge'),
    privilegeIcon: document.getElementById('privilegeIcon'),
    privilegeText: document.getElementById('privilegeText'),
    btnElevateAdmin: document.getElementById('btnElevateAdmin'),
    liveBadge: document.getElementById('liveBadge'),
    liveStatusText: document.getElementById('liveStatusText'),

    drivesGrid: document.getElementById('drivesGrid'),
    btnSelectAllDrives: document.getElementById('btnSelectAllDrives'),
    btnRefreshDrives: document.getElementById('btnRefreshDrives'),
    workerThreads: document.getElementById('workerThreads'),
    hashAlgo: document.getElementById('hashAlgo'),
    hashMode: document.getElementById('hashMode'),
    minFileSize: document.getElementById('minFileSize'),
    autoSaveInterval: document.getElementById('autoSaveInterval'),
    autoSaveBanner: document.getElementById('autoSaveBanner'),
    autoSaveDetailsText: document.getElementById('autoSaveDetailsText'),
    btnBannerRestore: document.getElementById('btnBannerRestore'),
    btnBannerQuickScan: document.getElementById('btnBannerQuickScan'),
    btnBannerDismiss: document.getElementById('btnBannerDismiss'),
    btnStartScan: document.getElementById('btnStartScan'),
    btnQuickScan: document.getElementById('btnQuickScan'),
    btnCancelScan: document.getElementById('btnCancelScan'),
    progressHUD: document.getElementById('progressHUD'),
    hudPhaseBadge: document.getElementById('hudPhaseBadge'),
    hudQuickScanBadge: document.getElementById('hudQuickScanBadge'),
    hudAutoSaveBadge: document.getElementById('hudAutoSaveBadge'),
    hudErrorsBadge: document.getElementById('hudErrorsBadge'),
    statFiles: document.getElementById('statFiles'),
    statDirs: document.getElementById('statDirs'),
    statBytes: document.getElementById('statBytes'),
    statAllocatedBytes: document.getElementById('statAllocatedBytes'),
    boxReusedFiles: document.getElementById('boxReusedFiles'),
    statReusedFiles: document.getElementById('statReusedFiles'),
    boxNewModifiedFiles: document.getElementById('boxNewModifiedFiles'),
    statNewModifiedFiles: document.getElementById('statNewModifiedFiles'),
    statCompressedCount: document.getElementById('statCompressedCount'),
    statCompressedSaved: document.getElementById('statCompressedSaved'),
    statSpeed: document.getElementById('statSpeed'),
    statElapsed: document.getElementById('statElapsed'),
    progressBarFill: document.getElementById('progressBarFill'),
    currentPathText: document.getElementById('currentPathText'),
    progressPercentText: document.getElementById('progressPercentText'),
    activeWorkersSection: document.getElementById('activeWorkersSection'),
    activeWorkersGrid: document.getElementById('activeWorkersGrid'),
    activeWorkerCountText: document.getElementById('activeWorkerCountText'),
    recentFilesTableBody: document.getElementById('recentFilesTableBody'),
    // Tree & Treemap Elements
    btnTreeGoUp: document.getElementById('btnTreeGoUp'),
    treeBreadcrumbs: document.getElementById('treeBreadcrumbs'),
    treeSplitLayout: document.getElementById('treeSplitLayout'),
    treemapColorMode: document.getElementById('treemapColorMode'),
    treemapDepth: document.getElementById('treemapDepth'),
    treemapDepthVal: document.getElementById('treemapDepthVal'),
    treeSearchInput: document.getElementById('treeSearchInput'),
    btnTreeRefresh: document.getElementById('btnTreeRefresh'),
    btnTreeMaxHeight: document.getElementById('btnTreeMaxHeight'),
    treeSplitter: document.getElementById('treeSplitter'),
    treeTableBody: document.getElementById('treeTableBody'),
    treemapCurrentTitle: document.getElementById('treemapCurrentTitle'),
    treemapCurrentSubtitle: document.getElementById('treemapCurrentSubtitle'),
    btnResetZoom: document.getElementById('btnResetZoom'),
    treemapContainer: document.getElementById('treemapContainer'),
    treemapCanvas: document.getElementById('treemapCanvas'),
    treemapLegend: document.getElementById('treemapLegend'),
    treemapTooltip: document.getElementById('treemapTooltip'),
    treemapContextMenu: document.getElementById('treemapContextMenu'),
    ctxZoomIn: document.getElementById('ctxZoomIn'),
    ctxZoomOut: document.getElementById('ctxZoomOut'),
    ctxCopyPath: document.getElementById('ctxCopyPath'),
    ctxRecycle: document.getElementById('ctxRecycle'),
    treemapLegendBar: document.getElementById('treemapLegendBar'),
    btnViewSplit: document.getElementById('btnViewSplit'),
    btnViewTreemap: document.getElementById('btnViewTreemap'),
    btnViewTable: document.getElementById('btnViewTable'),
    treemapFilterInput: document.getElementById('treemapFilterInput'),
    // Duplicates tab elements
    dupCountBadge: document.getElementById('dupCountBadge'),
    dupTotalGroups: document.getElementById('dupTotalGroups'),
    dupTotalFiles: document.getElementById('dupTotalFiles'),
    dupTotalWasted: document.getElementById('dupTotalWasted'),
    dupSelectedCount: document.getElementById('dupSelectedCount'),
    dupSortBy: document.getElementById('dupSortBy'),
    dupMinSize: document.getElementById('dupMinSize'),
    dupSearch: document.getElementById('dupSearch'),
    duplicatesSortBy: document.getElementById('duplicatesSortBy'),
    duplicatesMinSize: document.getElementById('duplicatesMinSize'),
    duplicatesExtFilter: document.getElementById('duplicatesExtFilter'),
    duplicatesSearch: document.getElementById('duplicatesSearch'),
    btnFilterDuplicates: document.getElementById('btnFilterDuplicates'),
    btnSelectNewest: document.getElementById('btnSelectNewest'),
    btnSelectOldest: document.getElementById('btnSelectOldest'),
    dupSelectAllOldestBtn: document.getElementById('dupSelectAllOldestBtn'),
    dupSelectAllNewestBtn: document.getElementById('dupSelectAllNewestBtn'),
    btnClearSelection: document.getElementById('btnClearSelection'),
    dupClearSelectionBtn: document.getElementById('dupClearSelectionBtn'),
    btnRecycleSelected: document.getElementById('btnRecycleSelected'),
    btnDeleteSelectedDups: document.getElementById('btnDeleteSelectedDups'),
    duplicatesContainer: document.getElementById('duplicatesContainer'),
    duplicatesList: document.getElementById('duplicatesList'),
    dupSummaryText: document.getElementById('dupSummaryText'),
    // Folder duplicates and compare elements
    folderDupCountBadge: document.getElementById('folderDupCountBadge'),
    dupFolderTotalGroups: document.getElementById('dupFolderTotalGroups'),
    dupFolderTotalCount: document.getElementById('dupFolderTotalCount'),
    dupFolderTotalWasted: document.getElementById('dupFolderTotalWasted'),
    dupFolderSortBy: document.getElementById('dupFolderSortBy'),
    dupFolderMinSize: document.getElementById('dupFolderMinSize'),
    dupFolderSearch: document.getElementById('dupFolderSearch'),
    chkFolderTopLevelOnly: document.getElementById('chkFolderTopLevelOnly'),
    btnRefreshFolderDuplicates: document.getElementById('btnRefreshFolderDuplicates'),
    folderDuplicatesContainer: document.getElementById('folderDuplicatesContainer'),
    folderDupMinSize: document.getElementById('folderDupMinSize'),
    folderDupSortBy: document.getElementById('folderDupSortBy'),
    folderDupSearch: document.getElementById('folderDupSearch'),
    btnFilterFolderDuplicates: document.getElementById('btnFilterFolderDuplicates'),
    folderDupSummaryText: document.getElementById('folderDupSummaryText'),
    comparePathA: document.getElementById('comparePathA'),
    comparePathB: document.getElementById('comparePathB'),
    btnSwapComparePaths: document.getElementById('btnSwapComparePaths'),
    btnRunFolderCompare: document.getElementById('btnRunFolderCompare'),
    folderCompareResults: document.getElementById('folderCompareResults'),
    // Idle files tab elements
    idleTotalCount: document.getElementById('idleTotalCount'),
    idleTotalBytes: document.getElementById('idleTotalBytes'),
    idleSelectedCount: document.getElementById('idleSelectedCount'),
    idleBucketsGrid: document.getElementById('idleBucketsGrid'),
    idleMinAge: document.getElementById('idleMinAge'),
    idleMinSize: document.getElementById('idleMinSize'),
    idleSearch: document.getElementById('idleSearch'),
    btnRefreshIdle: document.getElementById('btnRefreshIdle'),
    idleSummaryText: document.getElementById('idleSummaryText'),
    btnSelectAllIdle: document.getElementById('btnSelectAllIdle'),
    btnClearIdleSelection: document.getElementById('btnClearIdleSelection'),
    btnRecycleIdleSelected: document.getElementById('btnRecycleIdleSelected'),
    idleTableBody: document.getElementById('idleTableBody'),
    idleSelectAllCheckbox: document.getElementById('idleSelectAllCheckbox'),
    // Cache persistence elements
    btnOpenSaveCacheModal: document.getElementById('btnOpenSaveCacheModal'),
    btnOpenLoadCacheModal: document.getElementById('btnOpenLoadCacheModal'),
    saveCacheModal: document.getElementById('saveCacheModal'),
    loadCacheModal: document.getElementById('loadCacheModal'),
    saveCacheFileName: document.getElementById('saveCacheFileName'),
    btnConfirmSaveCache: document.getElementById('btnConfirmSaveCache'),
    savedCachesList: document.getElementById('savedCachesList'),
    customCachePath: document.getElementById('customCachePath'),
    btnLoadCustomCache: document.getElementById('btnLoadCustomCache'),
    // Analytics & watcher
    extensionsTableBody: document.getElementById('extensionsTableBody'),
    eventCountBadge: document.getElementById('eventCountBadge'),
    watcherStateTitle: document.getElementById('watcherStateTitle'),
    watcherStateDesc: document.getElementById('watcherStateDesc'),
    eventLogsTableBody: document.getElementById('eventLogsTableBody'),
    btnClearLogs: document.getElementById('btnClearLogs'),
    toastContainer: document.getElementById('toastContainer'),
    // AI Assistant Elements
    aiModelSelect: document.getElementById('aiModelSelect'),
    btnPullSelectedModel: document.getElementById('btnPullSelectedModel'),
    btnOpenAIConfigModal: document.getElementById('btnOpenAIConfigModal'),
    aiConfigModal: document.getElementById('aiConfigModal'),
    aiOllamaEndpointInput: document.getElementById('aiOllamaEndpointInput'),
    aiOpenRouterKeyInput: document.getElementById('aiOpenRouterKeyInput'),
    aiDryRunDefaultCheckbox: document.getElementById('aiDryRunDefaultCheckbox'),
    btnSaveAIConfig: document.getElementById('btnSaveAIConfig'),
    aiPullProgressContainer: document.getElementById('aiPullProgressContainer'),
    pullModelTitle: document.getElementById('pullModelTitle'),
    pullModelPercent: document.getElementById('pullModelPercent'),
    pullProgressBar: document.getElementById('pullProgressBar'),
    pullModelStatus: document.getElementById('pullModelStatus'),
    aiMessagesContainer: document.getElementById('aiMessagesContainer'),
    aiPromptInput: document.getElementById('aiPromptInput'),
    btnSendAIMessage: document.getElementById('btnSendAIMessage'),
    btnClearAIChat: document.getElementById('btnClearAIChat'),
    aiModelsList: document.getElementById('aiModelsList'),
    ollamaStatusDot: document.getElementById('ollamaStatusDot'),
    openrouterStatusDot: document.getElementById('openrouterStatusDot'),
    directStatusDot: document.getElementById('directStatusDot'),
    // Global operation progress overlay
    globalProgressOverlay: document.getElementById('globalProgressOverlay'),
    globalProgressTitle: document.getElementById('globalProgressTitle'),
    globalProgressDesc: document.getElementById('globalProgressDesc'),
    globalProgressBar: document.getElementById('globalProgressBar'),
    globalProgressPercent: document.getElementById('globalProgressPercent'),
    globalProgressDetail: document.getElementById('globalProgressDetail'),
  };

  // Helper: Format Bytes (e.g. 1024 -> 1.00 KB)
  function formatBytes(bytes, decimals = 2) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const dm = decimals < 0 ? 0 : decimals;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];
    const i = Math.floor(Math.log(Math.abs(bytes)) / Math.log(k));
    const val = (bytes / Math.pow(k, i)).toFixed(dm);
    return `${val} ${sizes[i] || 'B'}`;
  }

  // Helper: Format Numbers with commas
  function formatNumber(num) {
    if (num === null || num === undefined) return '0';
    return Number(num).toLocaleString('pt-BR');
  }

  // Set Global Operation Lock & Progress State
  function setGlobalOperationLock(isLocked, title = '', desc = '', percent = 0, detail = '') {
    state.isBusyOperation = isLocked;

    // Prevent duplicate triggers by disabling action buttons
    const actionButtons = [
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

    actionButtons.forEach(btn => {
      if (btn) {
        btn.disabled = isLocked;
        if (isLocked) {
          btn.classList.add('disabled-action');
        } else {
          btn.classList.remove('disabled-action');
        }
      }
    });

    // Toggle overlay
    if (elements.globalProgressOverlay) {
      if (isLocked) {
        elements.globalProgressOverlay.classList.remove('hidden');
        if (title && elements.globalProgressTitle) elements.globalProgressTitle.textContent = title;
        if (desc && elements.globalProgressDesc) elements.globalProgressDesc.textContent = desc;
        updateGlobalProgress(percent, detail);
      } else {
        elements.globalProgressOverlay.classList.add('hidden');
      }
    }
  }

  function updateGlobalProgress(percent = 0, detail = '') {
    const clamped = Math.max(0, Math.min(100, Math.round(percent)));
    if (elements.globalProgressBar) elements.globalProgressBar.style.width = `${clamped}%`;
    if (elements.globalProgressPercent) elements.globalProgressPercent.textContent = `${clamped}%`;
    if (detail && elements.globalProgressDetail) elements.globalProgressDetail.textContent = detail;
  }

  // Helper: Format Date
  function formatDate(unixSec) {
    if (!unixSec) return '-';
    return new Date(unixSec * 1000).toLocaleString('pt-BR');
  }

  // Helper: Toast Notifications
  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.innerHTML = `<span>${message}</span>`;
    elements.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      setTimeout(() => toast.remove(), 300);
    }, 4000);
  }

  // ==========================================
  // THEME MANAGER (12 THEMES: 4 OCHRE, 4 LIGHT, 4 DARK)
  // ==========================================
  function setupThemeManager() {
    const savedTheme = localStorage.getItem('scanfile_theme') || 'theme-ochre-dark';
    applyTheme(savedTheme, false);

    if (elements.themeSelector) {
      elements.themeSelector.value = state.theme || savedTheme;
      elements.themeSelector.addEventListener('change', (e) => {
        const newTheme = e.target.value;
        applyTheme(newTheme, true);
      });
    }
  }

  function applyTheme(theme, save = false) {
    if (!theme) theme = 'theme-ochre-dark';
    // Backwards compatibility mappings
    if (theme === 'ochre-dark') theme = 'theme-ochre-dark';
    else if (theme === 'ochre-light') theme = 'theme-ochre-sand';
    else if (theme === 'obsidian') theme = 'theme-dark-obsidian';
    else if (!theme.startsWith('theme-')) theme = 'theme-' + theme;

    // Remove any existing theme-* classes from body
    const classesToRemove = [];
    document.body.classList.forEach(cls => {
      if (cls.startsWith('theme-') || cls === 'dark-theme' || cls === 'light-theme') {
        classesToRemove.push(cls);
      }
    });
    classesToRemove.forEach(cls => document.body.classList.remove(cls));

    document.body.classList.add(theme);
    state.theme = theme;
    try {
      localStorage.setItem('scanfile_theme', theme);
    } catch (e) {}

    if (elements.themeSelector && elements.themeSelector.value !== theme) {
      elements.themeSelector.value = theme;
    }

    if (state.currentTab === 'treeTab') {
      setTimeout(() => resizeTreemapCanvas(), 50);
    }

    if (save && typeof saveCurrentConfig === 'function') {
      saveCurrentConfig();
    }
  }



  // ==========================================
  // REALTIME MEMORY TELEMETRY
  // ==========================================
  async function pollMemoryStats() {
    try {
      const res = await fetch('/api/system/memory');
      if (res.ok) {
        const data = await res.json();
        updateMemoryTelemetry(data);
      }
    } catch (e) {
      // Quiet background polling
    }
  }

  function updateMemoryTelemetry(mem) {
    if (!mem) return;
    const allocMB = mem.allocMB !== undefined ? mem.allocMB : (mem.AllocMB || 0);
    const sysMB = mem.sysMB !== undefined ? mem.sysMB : (mem.SysMB || 0);
    const sysTotalMB = mem.systemTotalRAMMB !== undefined ? mem.systemTotalRAMMB : (mem.SystemTotalRAMMB || 0);
    const sysUsedMB = mem.systemUsedRAMMB !== undefined ? mem.systemUsedRAMMB : (mem.SystemUsedRAMMB || 0);
    const sysTotalGB = (sysTotalMB / 1024).toFixed(1);
    const sysUsedGB = (sysUsedMB / 1024).toFixed(1);
    const sysPct = Math.round(mem.systemPercent !== undefined ? mem.systemPercent : (mem.SystemPercent || 0));

    if (elements.memAppUsage) {
      elements.memAppUsage.textContent = `${allocMB} MB (Heap: ${sysMB} MB)`;
    }
    if (elements.memSysUsage) {
      elements.memSysUsage.textContent = `SO: ${sysUsedGB} / ${sysTotalGB} GB (${sysPct}%)`;
    }
    if (elements.memoryBarFill) {
      elements.memoryBarFill.style.width = `${Math.min(100, Math.max(2, sysPct))}%`;
      // Change color based on RAM pressure
      if (sysPct > 85) {
        elements.memoryBarFill.style.background = 'linear-gradient(90deg, #f59e0b, #ef4444)';
      } else {
        elements.memoryBarFill.style.background = 'linear-gradient(90deg, #10b981, #f59e0b)';
      }
    }
    if (elements.footerMemoryText) {
      elements.footerMemoryText.textContent = `RAM App: ${allocMB} MB / SO: ${sysUsedGB} GB (${sysPct}%)`;
    }
  }

  // ==========================================
  // UNIVERSAL HIGH-PERFORMANCE PAGINATOR
  // ==========================================
  function renderPaginationBar(container, totalItems, currentPage, pageSize, onPageChange, onPageSizeChange) {
    if (!container) return;
    if (totalItems <= 0) {
      container.innerHTML = '';
      return;
    }

    const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
    const startItem = (currentPage - 1) * pageSize + 1;
    const endItem = Math.min(totalItems, currentPage * pageSize);

    let pagesHtml = '';
    const maxButtons = 5;
    let startPage = Math.max(1, currentPage - Math.floor(maxButtons / 2));
    let endPage = Math.min(totalPages, startPage + maxButtons - 1);
    if (endPage - startPage + 1 < maxButtons) {
      startPage = Math.max(1, endPage - maxButtons + 1);
    }

    if (startPage > 1) {
      pagesHtml += `<button class="pagination-page-btn" data-page="1">1</button>`;
      if (startPage > 2) {
        pagesHtml += `<span style="color:var(--text-muted); padding:0 3px;">...</span>`;
      }
    }

    for (let p = startPage; p <= endPage; p++) {
      pagesHtml += `<button class="pagination-page-btn ${p === currentPage ? 'active' : ''}" data-page="${p}">${p}</button>`;
    }

    if (endPage < totalPages) {
      if (endPage < totalPages - 1) {
        pagesHtml += `<span style="color:var(--text-muted); padding:0 3px;">...</span>`;
      }
      pagesHtml += `<button class="pagination-page-btn" data-page="${totalPages}">${totalPages}</button>`;
    }

    container.innerHTML = `
      <div class="pagination-left">
        <span class="pagination-info">Exibindo <strong>${formatNumber(startItem)}</strong> - <strong>${formatNumber(endItem)}</strong> de <strong>${formatNumber(totalItems)}</strong> itens</span>
      </div>
      <div class="pagination-right">
        <div class="pagination-pages">
          <button class="pagination-btn" id="pgBtnFirst" ${currentPage <= 1 ? 'disabled' : ''} title="Primeira Página">«</button>
          <button class="pagination-btn" id="pgBtnPrev" ${currentPage <= 1 ? 'disabled' : ''} title="Página Anterior">‹</button>
          ${pagesHtml}
          <button class="pagination-btn" id="pgBtnNext" ${currentPage >= totalPages ? 'disabled' : ''} title="Próxima Página">›</button>
          <button class="pagination-btn" id="pgBtnLast" ${currentPage >= totalPages ? 'disabled' : ''} title="Última Página">»</button>
        </div>
        <select class="pagination-select" id="pgSelectLimit" title="Itens por página">
          <option value="25" ${pageSize === 25 ? 'selected' : ''}>25 / pág</option>
          <option value="50" ${pageSize === 50 ? 'selected' : ''}>50 / pág</option>
          <option value="100" ${pageSize === 100 ? 'selected' : ''}>100 / pág</option>
          <option value="250" ${pageSize === 250 ? 'selected' : ''}>250 / pág</option>
          <option value="500" ${pageSize === 500 ? 'selected' : ''}>500 / pág</option>
        </select>
      </div>
    `;

    container.querySelectorAll('.pagination-page-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        const page = parseInt(btn.getAttribute('data-page'), 10);
        if (page && page !== currentPage) onPageChange(page);
      });
    });

    const btnFirst = container.querySelector('#pgBtnFirst');
    if (btnFirst) btnFirst.addEventListener('click', () => onPageChange(1));

    const btnPrev = container.querySelector('#pgBtnPrev');
    if (btnPrev) btnPrev.addEventListener('click', () => onPageChange(currentPage - 1));

    const btnNext = container.querySelector('#pgBtnNext');
    if (btnNext) btnNext.addEventListener('click', () => onPageChange(currentPage + 1));

    const btnLast = container.querySelector('#pgBtnLast');
    if (btnLast) btnLast.addEventListener('click', () => onPageChange(totalPages));

    const selectLimit = container.querySelector('#pgSelectLimit');
    if (selectLimit) {
      selectLimit.addEventListener('change', (e) => {
        const newLimit = parseInt(e.target.value, 10);
        if (newLimit && onPageSizeChange) onPageSizeChange(newLimit);
      });
    }
  }

  // Initialize
  async function init() {
    // Global Safety Guard against browser crash / STATUS_BREAKPOINT
    window.addEventListener('error', (e) => {
      console.warn('[ScanFile Safety Guard] Exceção capturada preventivamente:', e.error || e.message);
    });
    window.addEventListener('unhandledrejection', (e) => {
      console.warn('[ScanFile Safety Guard] Rejeição assíncrona capturada:', e.reason);
    });

    try { setupThemeManager(); } catch (e) { console.error('Error in setupThemeManager:', e); }
    try { setupTabs(); } catch (e) { console.error('Error in setupTabs:', e); }
    try { setupEventListeners(); } catch (e) { console.error('Error in setupEventListeners:', e); }
    try { setupTreemap(); } catch (e) { console.error('Error in setupTreemap:', e); }
    try { setupCacheModals(); } catch (e) { console.error('Error in setupCacheModals:', e); }
    try { setupFolderComparator(); } catch (e) { console.error('Error in setupFolderComparator:', e); }
    try { setupAIAssistant(); } catch (e) { console.error('Error in setupAIAssistant:', e); }
    try { fetchPrivileges(); } catch (e) { console.error('Error in fetchPrivileges:', e); }
    try { await loadSavedConfig(); } catch (e) { console.error('Error in loadSavedConfig:', e); }
    try { fetchDrives(); } catch (e) { console.error('Error in fetchDrives:', e); }
    try { setupSSE(); } catch (e) { console.error('Error in setupSSE:', e); }
    try { fetchEventLogs(); } catch (e) { console.error('Error in fetchEventLogs:', e); }
    try { checkAutoSaveStatus(); } catch (e) { console.error('Error in checkAutoSaveStatus:', e); }
    try { pollMemoryStats(); setInterval(pollMemoryStats, 2500); } catch (e) { console.error('Error in pollMemoryStats:', e); }
  }


  // Tabs Switching
  function setupTabs() {
    document.querySelectorAll('.nav-tab').forEach(btn => {
      btn.addEventListener('click', () => {
        const targetTab = btn.getAttribute('data-tab');
        const targetPane = document.getElementById(targetTab);
        if (!targetPane) return;

        document.querySelectorAll('.nav-tab').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
        
        btn.classList.add('active');
        targetPane.classList.add('active');
        state.currentTab = targetTab;

        if (targetTab === 'treeTab') {
          loadTreeData(state.treePath);
          setTimeout(() => resizeTreemapCanvas(), 50);
        } else if (targetTab === 'duplicatesTab') {
          loadDuplicates();
        } else if (targetTab === 'foldersTab') {
          loadFolderDuplicates();
        } else if (targetTab === 'analyticsTab') {
          loadAnalytics();
        } else if (targetTab === 'idleTab') {
          loadIdleFiles();
        } else if (targetTab === 'aiTab') {
          loadAICatalog();
        }
      });
    });
  }

  // Setup Event Listeners
  function setupEventListeners() {
    if (elements.btnElevateAdmin) {
      elements.btnElevateAdmin.addEventListener('click', elevateToAdmin);
    }
    if (elements.btnRefreshDrives) {
      elements.btnRefreshDrives.addEventListener('click', fetchDrives);
    }
    
    if (elements.btnSelectAllDrives) {
      elements.btnSelectAllDrives.addEventListener('click', () => {
        if (!state.drives) return;
        const allSelected = state.selectedRoots.size === state.drives.length;
        state.selectedRoots.clear();
        if (!allSelected) {
          state.drives.forEach(d => state.selectedRoots.add(d.letter));
        }
        renderDrivesGrid();
        saveCurrentConfig();
      });
    }

    if (elements.btnStartScan) {
      elements.btnStartScan.addEventListener('click', () => {
        saveCurrentConfig();
        startScan(false);
      });
    }

    if (elements.btnQuickScan) {
      elements.btnQuickScan.addEventListener('click', () => {
        saveCurrentConfig();
        startScan(true);
      });
    }

    // AutoSave Detection Banner Event Listeners
    if (elements.btnBannerRestore) {
      elements.btnBannerRestore.addEventListener('click', () => restoreAutoSave());
    }
    if (elements.btnBannerQuickScan) {
      elements.btnBannerQuickScan.addEventListener('click', async () => {
        try {
          await restoreAutoSave();
          startScan(true);
        } catch (e) {
          console.error(e);
        }
      });
    }
    if (elements.btnBannerDismiss) {
      elements.btnBannerDismiss.addEventListener('click', () => {
        if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
      });
    }

    if (elements.btnCancelScan) {
      elements.btnCancelScan.addEventListener('click', cancelScan);
    }

    // Save preferences on scan configuration change
    if (elements.workerThreads) elements.workerThreads.addEventListener('change', () => saveCurrentConfig());
    if (elements.hashAlgo) elements.hashAlgo.addEventListener('change', () => saveCurrentConfig());
    if (elements.hashMode) elements.hashMode.addEventListener('change', () => saveCurrentConfig());
    if (elements.minFileSize) elements.minFileSize.addEventListener('change', () => saveCurrentConfig());
    if (elements.autoSaveInterval) elements.autoSaveInterval.addEventListener('change', () => saveCurrentConfig());

    // Tree and Treemap Navigation Listeners
    if (elements.btnTreeRefresh) {
      elements.btnTreeRefresh.addEventListener('click', () => loadTreeData(state.treePath));
    }
    if (elements.treeSearchInput) {
      elements.treeSearchInput.addEventListener('input', debounce(() => renderTreeTable(), 250));
    }
    if (elements.btnTreeGoUp) {
      elements.btnTreeGoUp.addEventListener('click', treeGoUp);
    }
    if (elements.btnResetZoom) {
      elements.btnResetZoom.addEventListener('click', () => loadTreeData(''));
    }

    // View Mode Switcher
    document.querySelectorAll('[data-tree-view]').forEach(btn => {
      btn.addEventListener('click', () => {
        const view = btn.getAttribute('data-tree-view');
        document.querySelectorAll('[data-tree-view]').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        state.treemap.viewMode = view;

        if (elements.treeSplitLayout) {
          elements.treeSplitLayout.className = `tree-split-layout view-${view}`;
        }
        setTimeout(() => resizeTreemapCanvas(), 80);
        saveCurrentConfig();
      });
    });

    // Color Mode Switcher
    if (elements.treemapColorMode) {
      elements.treemapColorMode.addEventListener('change', (e) => {
        state.treemap.colorMode = e.target.value;
        renderTreemapCanvas();
        renderTreemapLegend();
        saveCurrentConfig();
      });
    }

    // Depth Slider (2 to 20)
    if (elements.treemapDepth) {
      elements.treemapDepth.addEventListener('input', (e) => {
        const val = parseInt(e.target.value, 10);
        state.treemap.depth = val;
        if (elements.treemapDepthVal) {
          elements.treemapDepthVal.textContent = `${val} níveis`;
        }
      });
      elements.treemapDepth.addEventListener('change', (e) => {
        loadTreeData(state.treePath);
        saveCurrentConfig();
      });
    }

    if (elements.dupSortBy) {
      elements.dupSortBy.addEventListener('change', () => {
        loadDuplicates();
        saveCurrentConfig();
      });
    }
    if (elements.dupMinSize) {
      elements.dupMinSize.addEventListener('change', () => {
        loadDuplicates();
        saveCurrentConfig();
      });
    }
    if (elements.dupSearch) {
      elements.dupSearch.addEventListener('input', debounce(loadDuplicates, 300));
    }

    if (elements.btnSelectNewest) elements.btnSelectNewest.addEventListener('click', () => selectDuplicatesByStrategy('keep_newest'));
    if (elements.btnSelectOldest) elements.btnSelectOldest.addEventListener('click', () => selectDuplicatesByStrategy('keep_oldest'));
    if (elements.btnClearSelection) elements.btnClearSelection.addEventListener('click', clearSelection);
    if (elements.btnRecycleSelected) elements.btnRecycleSelected.addEventListener('click', recycleSelectedFiles);

    // Idle files listeners
    if (elements.idleMinAge) elements.idleMinAge.addEventListener('change', () => {
      loadIdleFiles(1);
      saveCurrentConfig();
    });
    if (elements.idleMinSize) elements.idleMinSize.addEventListener('change', () => {
      loadIdleFiles(1);
      saveCurrentConfig();
    });
    if (elements.idleSortBy) elements.idleSortBy.addEventListener('change', () => {
      loadIdleFiles(1);
      saveCurrentConfig();
    });
    if (elements.idleSearch) elements.idleSearch.addEventListener('input', debounce(() => loadIdleFiles(1), 300));
    if (elements.btnRefreshIdle) elements.btnRefreshIdle.addEventListener('click', () => loadIdleFiles(1));
    if (elements.btnSelectAllIdle) elements.btnSelectAllIdle.addEventListener('click', selectAllIdleFiles);
    if (elements.btnClearIdleSelection) elements.btnClearIdleSelection.addEventListener('click', clearIdleSelection);
    if (elements.btnRecycleIdleSelected) elements.btnRecycleIdleSelected.addEventListener('click', recycleIdleSelectedFiles);
    if (elements.idleSelectAllCheckbox) {
      elements.idleSelectAllCheckbox.addEventListener('change', (e) => {
        if (e.target.checked) selectAllIdleFiles();
        else clearIdleSelection();
      });
    }

    // Folder Duplicate listeners
    if (elements.dupFolderSortBy) elements.dupFolderSortBy.addEventListener('change', () => {
      loadFolderDuplicates(1);
      saveCurrentConfig();
    });
    if (elements.dupFolderMinSize) elements.dupFolderMinSize.addEventListener('change', () => {
      loadFolderDuplicates(1);
      saveCurrentConfig();
    });
    if (elements.dupFolderSearch) elements.dupFolderSearch.addEventListener('input', debounce(() => loadFolderDuplicates(1), 300));
    if (elements.chkFolderTopLevelOnly) elements.chkFolderTopLevelOnly.addEventListener('change', () => {
      loadFolderDuplicates(1);
      saveCurrentConfig();
    });
    if (elements.btnRefreshFolderDuplicates) elements.btnRefreshFolderDuplicates.addEventListener('click', () => loadFolderDuplicates(1));
    if (elements.comparePathA) elements.comparePathA.addEventListener('change', () => saveCurrentConfig());
    if (elements.comparePathB) elements.comparePathB.addEventListener('change', () => saveCurrentConfig());


    // UI Zoom Control Listeners
    if (elements.btnZoomIn) {
      elements.btnZoomIn.addEventListener('click', () => setUIZoom((state.uiZoom || 100) + 5));
    }
    if (elements.btnZoomOut) {
      elements.btnZoomOut.addEventListener('click', () => setUIZoom((state.uiZoom || 100) - 5));
    }
    if (elements.zoomLevelDisplay) {
      elements.zoomLevelDisplay.addEventListener('click', () => setUIZoom(100));
    }

    // Keyboard Shortcuts for Zoom (Ctrl +, Ctrl -, Ctrl 0)
    window.addEventListener('keydown', (e) => {
      if (e.ctrlKey || e.metaKey) {
        if (e.key === '=' || e.key === '+') {
          e.preventDefault();
          setUIZoom((state.uiZoom || 100) + 5);
        } else if (e.key === '-' || e.key === '_') {
          e.preventDefault();
          setUIZoom((state.uiZoom || 100) - 5);
        } else if (e.key === '0') {
          e.preventDefault();
          setUIZoom(100);
        }
      }
    });

    if (elements.btnClearLogs) {
      elements.btnClearLogs.addEventListener('click', () => {
        state.eventLogs = [];
        renderEventLogs();
        if (elements.eventCountBadge) elements.eventCountBadge.textContent = '0';
      });
    }
  }

  // Set UI Zoom percentage
  function setUIZoom(percent, save = true) {
    const clamped = Math.max(60, Math.min(180, Math.round(percent || 100)));
    state.uiZoom = clamped;
    document.body.style.zoom = `${clamped}%`;

    if (elements.zoomLevelDisplay) {
      elements.zoomLevelDisplay.textContent = `${clamped}%`;
    }

    setTimeout(() => {
      resizeTreemapCanvas();
    }, 60);

    if (save) {
      saveCurrentConfig();
    }
  }

  // Debounce utility
  function debounce(func, wait) {
    let timeout;
    return function (...args) {
      clearTimeout(timeout);
      timeout = setTimeout(() => func.apply(this, args), wait);
    };
  }

  // Load User Saved Configuration Preferences
  async function loadSavedConfig() {
    try {
      const res = await fetch('/api/config');
      if (!res.ok) return;
      const cfg = await res.json();
      if (!cfg) return;

      if (cfg.theme) {
        applyTheme(cfg.theme, false);
      }

      if (cfg.uiZoom) {
        setUIZoom(cfg.uiZoom, false);
      }

      if (cfg.workerThreads && elements.workerThreads) elements.workerThreads.value = String(cfg.workerThreads);
      if (cfg.hashAlgorithm && elements.hashAlgo) elements.hashAlgo.value = cfg.hashAlgorithm;
      if (cfg.hashMode && elements.hashMode) elements.hashMode.value = cfg.hashMode;
      if (cfg.minFileSize !== undefined && elements.minFileSize) elements.minFileSize.value = String(cfg.minFileSize);
      if (cfg.autoSaveIntervalMinutes !== undefined && elements.autoSaveInterval) elements.autoSaveInterval.value = String(cfg.autoSaveIntervalMinutes);

      if (cfg.treemapDepth && elements.treemapDepth) {
        elements.treemapDepth.value = String(cfg.treemapDepth);
        state.treemap.depth = cfg.treemapDepth;
        if (elements.treemapDepthVal) elements.treemapDepthVal.textContent = `${cfg.treemapDepth} níveis`;
      }
      if (cfg.treemapColorMode && elements.treemapColorMode) {
        elements.treemapColorMode.value = cfg.treemapColorMode;
        state.treemap.colorMode = cfg.treemapColorMode;
      }
      if (cfg.treemapViewMode) {
        state.treemap.viewMode = cfg.treemapViewMode;
        document.querySelectorAll('[data-tree-view]').forEach(b => {
          b.classList.toggle('active', b.getAttribute('data-tree-view') === cfg.treemapViewMode);
        });
        if (elements.treeSplitLayout) {
          elements.treeSplitLayout.className = `tree-split-layout view-${cfg.treemapViewMode}`;
        }
      }
      if (cfg.treeTableLimit) {
        state.treeTableLimit = cfg.treeTableLimit;
      }

      if (cfg.duplicatesSortBy && elements.dupSortBy) elements.dupSortBy.value = cfg.duplicatesSortBy;
      if (cfg.duplicatesMinSize !== undefined && elements.dupMinSize) elements.dupMinSize.value = String(cfg.duplicatesMinSize);
      if (cfg.dupLimit) state.dupLimit = cfg.dupLimit;

      if (cfg.idleMinAgeDays && elements.idleMinAge) elements.idleMinAge.value = String(cfg.idleMinAgeDays);
      if (cfg.idleMinSizeBytes !== undefined && elements.idleMinSize) elements.idleMinSize.value = String(cfg.idleMinSizeBytes);
      if (cfg.idleSortBy && elements.idleSortBy) elements.idleSortBy.value = cfg.idleSortBy;
      if (cfg.idleLimit) state.idleLimit = cfg.idleLimit;

      if (cfg.folderSortBy && elements.dupFolderSortBy) elements.dupFolderSortBy.value = cfg.folderSortBy;
      if (cfg.folderMinSize !== undefined && elements.dupFolderMinSize) elements.dupFolderMinSize.value = String(cfg.folderMinSize);
      if (cfg.folderDupLimit) state.folderDupLimit = cfg.folderDupLimit;
      if (cfg.chkFolderTopLevelOnly !== undefined && elements.chkFolderTopLevelOnly) {
        elements.chkFolderTopLevelOnly.checked = !!cfg.chkFolderTopLevelOnly;
      }

      if (cfg.comparePathA && elements.comparePathA) elements.comparePathA.value = cfg.comparePathA;
      if (cfg.comparePathB && elements.comparePathB) elements.comparePathB.value = cfg.comparePathB;

      if (cfg.selectedRoots && Array.isArray(cfg.selectedRoots) && cfg.selectedRoots.length > 0) {
        state.selectedRoots = new Set(cfg.selectedRoots);
      }
    } catch (e) {
      console.warn('Erro ao carregar preferências:', e);
    }
  }

  // Save User Configuration Preferences to scanfile_config.json
  const saveCurrentConfig = debounce(async function () {
    const payload = {
      theme: state.theme || localStorage.getItem('scanfile_theme') || 'theme-ochre-dark',
      selectedRoots: Array.from(state.selectedRoots),
      workerThreads: elements.workerThreads ? parseInt(elements.workerThreads.value, 10) : 8,
      hashAlgorithm: elements.hashAlgo ? elements.hashAlgo.value : 'xxhash',
      hashMode: elements.hashMode ? elements.hashMode.value : 'smart',
      minFileSize: elements.minFileSize ? parseInt(elements.minFileSize.value, 10) : 1,
      autoSaveIntervalMinutes: elements.autoSaveInterval ? parseInt(elements.autoSaveInterval.value, 10) : 5,
      treemapDepth: state.treemap ? state.treemap.depth : 5,
      treemapColorMode: state.treemap ? state.treemap.colorMode : 'extension',
      treemapViewMode: state.treemap ? state.treemap.viewMode : 'split',
      treeTableLimit: state.treeTableLimit || 50,
      duplicatesSortBy: elements.dupSortBy ? elements.dupSortBy.value : 'size_desc',
      duplicatesMinSize: elements.dupMinSize ? parseInt(elements.dupMinSize.value, 10) : 0,
      dupLimit: state.dupLimit || 50,
      idleMinAgeDays: elements.idleMinAge ? parseInt(elements.idleMinAge.value, 10) : 365,
      idleMinSizeBytes: elements.idleMinSize ? parseInt(elements.idleMinSize.value, 10) : 104857600,
      idleSortBy: elements.idleSortBy ? elements.idleSortBy.value : 'size_desc',
      idleLimit: state.idleLimit || 50,
      folderSortBy: elements.dupFolderSortBy ? elements.dupFolderSortBy.value : 'wasted_desc',
      folderMinSize: elements.dupFolderMinSize ? parseInt(elements.dupFolderMinSize.value, 10) : 0,
      folderDupLimit: state.folderDupLimit || 50,
      chkFolderTopLevelOnly: elements.chkFolderTopLevelOnly ? elements.chkFolderTopLevelOnly.checked : false,
      comparePathA: elements.comparePathA ? elements.comparePathA.value.trim() : '',
      comparePathB: elements.comparePathB ? elements.comparePathB.value.trim() : '',
      uiZoom: state.uiZoom || 100,
    };

    try {
      await fetch('/api/config', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });
    } catch (e) {
      console.warn('Erro ao salvar preferências:', e);
    }
  }, 250);


  // Fetch Windows Privileges & Elevation Status
  async function fetchPrivileges() {
    try {
      const res = await fetch('/api/system/privileges');
      if (!res.ok) return;
      const data = await res.json();

      if (data.isElevated) {
        elements.privilegeBadge.className = 'privilege-badge admin';
        elements.privilegeIcon.textContent = '👑';
        elements.privilegeText.textContent = data.hasBackupAccess 
          ? 'Administrador (SeBackupPrivilege)' 
          : 'Administrador (Elevado)';
        elements.privilegeBadge.title = `Executando como Administrador (${data.activeUser}). SeBackupPrivilege ativo: bypass total de permissões do NTFS.`;
        elements.btnElevateAdmin.classList.add('hidden');
      } else {
        elements.privilegeBadge.className = 'privilege-badge standard';
        elements.privilegeIcon.textContent = '🛡️';
        elements.privilegeText.textContent = 'Usuário Padrão';
        elements.privilegeBadge.title = `Executando em modo padrão. Clique em Elevar para abrir como Administrador com permissões máximas de disco.`;
        elements.btnElevateAdmin.classList.remove('hidden');
      }
    } catch (e) {
      console.warn('Erro ao consultar privilégios:', e);
    }
  }

  // Elevate Process to Admin via UAC
  async function elevateToAdmin() {
    try {
      showToast('Solicitando elevação de privilégios UAC ao Windows...', 'info');
      const res = await fetch('/api/system/elevate', { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
      showToast('Nova instância de Administrador solicitada via prompt UAC do Windows!', 'success');
    } catch (err) {
      showToast('Falha na elevação: ' + err.message, 'danger');
    }
  }

  // Fetch Drives
  async function fetchDrives() {
    if (!elements.drivesGrid) return;
    try {
      elements.drivesGrid.innerHTML = '<div class="loading-state"><span class="pulse-dot" style="display:inline-block; margin-right:8px;"></span>Detectando unidades de disco do sistema...</div>';
      
      const controller = new AbortController();
      const timeoutId = setTimeout(() => controller.abort(), 6000);
      
      const res = await fetch('/api/drives', { signal: controller.signal });
      clearTimeout(timeoutId);
      
      if (!res.ok) throw new Error('Falha ao obter discos do servidor.');
      const data = await res.json();
      state.drives = Array.isArray(data) ? data : [];
      
      // Auto-select all fixed/removable drives by default if none selected
      if (state.selectedRoots.size === 0) {
        state.drives.forEach(d => {
          if ((d.driveType && (d.driveType.includes('Fixed') || d.driveType.includes('Removable'))) || state.drives.length === 1) {
            state.selectedRoots.add(d.letter);
          }
        });
      }
      renderDrivesGrid();
    } catch (err) {
      console.warn('Erro ao obter discos:', err);
      elements.drivesGrid.innerHTML = `
        <div class="empty-state" style="padding: 20px; text-align: center;">
          <p style="color: var(--accent-rose); font-weight: 600; margin-bottom: 6px;">Não foi possível consultar automaticamente as unidades de disco.</p>
          <p style="color: var(--text-secondary); font-size: 12px; margin-bottom: 14px;">${err.message}</p>
          <button id="btnRetryDrives" class="btn btn-secondary btn-sm" onclick="window.__scanfile_retryDrives && window.__scanfile_retryDrives()">
            🔄 Tentar Novamente
          </button>
        </div>`;
      window.__scanfile_retryDrives = fetchDrives;
    }
  }


  // Render Drives Grid
  function renderDrivesGrid() {
    if (!state.drives || state.drives.length === 0) {
      elements.drivesGrid.innerHTML = '<div class="empty-state">Nenhum disco detectado.</div>';
      return;
    }

    elements.drivesGrid.innerHTML = state.drives.map(drive => {
      const isSelected = state.selectedRoots.has(drive.letter);
      const isHighUsage = drive.usedPercent > 85;

      return `
        <div class="drive-card ${isSelected ? 'selected' : ''}" data-letter="${drive.letter}">
          <div class="drive-card-header">
            <div class="drive-name-row">
              <div class="drive-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                  <rect x="2" y="2" width="20" height="8" rx="2" ry="2"></rect>
                  <rect x="2" y="14" width="20" height="8" rx="2" ry="2"></rect>
                  <line x1="6" y1="6" x2="6.01" y2="6"></line>
                  <line x1="6" y1="18" x2="6.01" y2="18"></line>
                </svg>
              </div>
              <div>
                <div class="drive-letter">${drive.letter}</div>
                <div class="drive-label">${drive.volumeLabel || 'Disco Local'} (${drive.fileSystem || 'NTFS'})</div>
              </div>
            </div>
            <input type="checkbox" class="drive-checkbox" ${isSelected ? 'checked' : ''} style="pointer-events: none;">
          </div>

          <div class="drive-meta-row">
            <span>${drive.driveType}</span>
            <span>${drive.usedPercent.toFixed(1)}% Usado</span>
          </div>

          <div class="drive-bar-track">
            <div class="drive-bar-fill ${isHighUsage ? 'danger' : ''}" style="width: ${Math.min(100, drive.usedPercent)}%"></div>
          </div>

          <div class="drive-space-text">
            <span>Livre: ${formatBytes(drive.freeBytes)}</span>
            <span>Total: ${formatBytes(drive.totalBytes)}</span>
          </div>
        </div>
      `;
    }).join('');

    // Attach card click handlers
    elements.drivesGrid.querySelectorAll('.drive-card').forEach(card => {
      card.addEventListener('click', () => {
        const letter = card.getAttribute('data-letter');
        if (state.selectedRoots.has(letter)) {
          state.selectedRoots.delete(letter);
        } else {
          state.selectedRoots.add(letter);
        }
        renderDrivesGrid();
        saveCurrentConfig();
      });
    });
  }

  // Start Scan (Regular or QuickScan)
  async function startScan(isQuick = false) {
    if (state.selectedRoots.size === 0) {
      document.querySelectorAll('.drive-card.selected').forEach(c => {
        const l = c.getAttribute('data-letter');
        if (l) state.selectedRoots.add(l);
      });
    }

    if (state.selectedRoots.size === 0) {
      showToast('Por favor, selecione ao menos uma unidade de disco para varrer!', 'warning');
      return;
    }

    const autoSaveMins = elements.autoSaveInterval ? parseInt(elements.autoSaveInterval.value, 10) : 5;

    const payload = {
      roots: Array.from(state.selectedRoots),
      workerThreads: parseInt(elements.workerThreads.value, 10),
      hashAlgorithm: elements.hashAlgo.value,
      hashAllFiles: elements.hashMode.value === 'all',
      minSizeForHash: parseInt(elements.minFileSize.value, 10),
      quickScanMode: isQuick,
      autoSaveIntervalMinutes: autoSaveMins,
      autoSaveOnComplete: true,
    };

    try {
      const res = await fetch('/api/scan/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!res.ok) throw new Error(await res.text());

      state.isScanning = true;
      elements.btnStartScan.classList.add('hidden');
      if (elements.btnQuickScan) elements.btnQuickScan.classList.add('hidden');
      elements.btnCancelScan.classList.remove('hidden');
      elements.progressHUD.classList.remove('hidden');
      if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = isQuick ? '⚡ Quick Scan...' : 'Varrendo Discos...';

      if (isQuick) {
        showToast('⚡ Quick Scan iniciado! Reaproveitando hashes de arquivos inalterados.', 'info');
      } else {
        showToast('Varredura multithread iniciada com sucesso!', 'success');
      }
    } catch (err) {
      showToast('Erro ao iniciar varredura: ' + err.message, 'danger');
    }
  }

  // Cancel Scan
  async function cancelScan() {
    try {
      await fetch('/api/scan/cancel', { method: 'POST' });
      showToast('Varredura cancelada pelo usuário.', 'info');
      elements.btnStartScan.classList.remove('hidden');
      if (elements.btnQuickScan) elements.btnQuickScan.classList.remove('hidden');
      elements.btnCancelScan.classList.add('hidden');
    } catch (err) {
      console.error(err);
    }
  }

  // Check and display AutoSave Banner on startup
  async function checkAutoSaveStatus() {
    try {
      const res = await fetch('/api/cache/autosave/status');
      if (!res.ok) return;
      const data = await res.json();
      if (data.exists && data.autoSave && elements.autoSaveBanner) {
        const info = data.autoSave;
        const d = new Date(info.modTime);
        const timeAgo = formatTimeAgo(d);
        elements.autoSaveDetailsText.textContent = `Arquivo: ${info.fileName} (${formatBytes(info.sizeBytes)}) • Salvo ${timeAgo}`;
        elements.autoSaveBanner.classList.remove('hidden');
      }
    } catch (err) {
      console.warn('Erro ao verificar status de autosave:', err);
    }
  }

  // Restore latest AutoSave snapshot
  async function restoreAutoSave() {
    try {
      setGlobalOperationLock(
        true, 
        'Restaurando Snapshot de AutoSave...', 
        'Descompactando arquivo de autosave e reconstruindo árvore de diretórios em memória...', 
        10, 
        'Iniciando leitura do arquivo compactado .sfz...'
      );

      showToast('Carregando autosave do disco...', 'info');
      const res = await fetch('/api/cache/autosave/restore', { method: 'POST' });
      if (!res.ok) throw new Error(await res.text());
      const result = await res.json();
      const snap = result.snapshot;

      updateGlobalProgress(100, 'Snapshot restaurado com sucesso!');
      if (elements.autoSaveBanner) elements.autoSaveBanner.classList.add('hidden');
      showToast(`Autosave restaurado com sucesso! ${formatNumber(snap.totalFiles)} arquivos carregados.`, 'success');

      // Auto check roots that were in snapshot
      if (snap.roots && snap.roots.length > 0) {
        state.selectedRoots.clear();
        snap.roots.forEach(r => state.selectedRoots.add(r));
        elements.drivesGrid.querySelectorAll('.drive-card').forEach(card => {
          const mount = card.getAttribute('data-mount');
          if (state.selectedRoots.has(mount)) {
            card.classList.add('selected');
          }
        });
      }

      loadTreeData('');
      loadDuplicates(true);
      loadFolderDuplicates(true);
      loadAnalytics();
      return snap;
    } catch (err) {
      showToast('Erro ao restaurar autosave: ' + err.message, 'danger');
      throw err;
    } finally {
      setTimeout(() => setGlobalOperationLock(false), 350);
    }
  }

  function formatTimeAgo(date) {
    const diff = Math.floor((new Date() - date) / 1000);
    if (diff < 60) return 'há poucos segundos';
    if (diff < 3600) return `há ${Math.floor(diff / 60)} min`;
    if (diff < 86400) return `há ${Math.floor(diff / 3600)} horas`;
    return `em ${date.toLocaleDateString()}`;
  }

  // Setup Server-Sent Events (SSE)
  function setupSSE() {
    if (state.sseSource) {
      state.sseSource.close();
    }

    state.sseSource = new EventSource('/api/events');

    state.sseSource.addEventListener('scan_progress', (e) => {
      const data = JSON.parse(e.data);
      updateScanProgress(data);
    });

    state.sseSource.addEventListener('autosave_done', (e) => {
      const data = JSON.parse(e.data);
      if (elements.hudAutoSaveBadge) {
        const d = new Date(data.time * 1000);
        elements.hudAutoSaveBadge.textContent = `💾 Auto-Salvo (${d.toLocaleTimeString()})`;
        elements.hudAutoSaveBadge.classList.remove('hidden');
      }
      showToast('💾 Cache salvo automaticamente em segundo plano!', 'success');
    });

    state.sseSource.addEventListener('fs_event', (e) => {
      const eventLog = JSON.parse(e.data);
      addFSEvent(eventLog);
    });

    state.sseSource.onerror = () => {
      // Reconnect handled automatically by EventSource
    };
  }

  // Update Scan Progress HUD
  function updateScanProgress(st) {
    if (!st) return;

    if (st.memoryStats || st.MemoryStats) {
      updateMemoryTelemetry(st.memoryStats || st.MemoryStats);
    }

    if (elements.footerStatusText) {
      elements.footerStatusText.textContent = st.isScanning ? `Varredura ativa (${st.phase || 'processando'})` : (st.isWatching ? 'Monitoramento em Tempo Real' : 'Pronto para varredura');
    }
    if (elements.footerRootsCount) {
      elements.footerRootsCount.textContent = `${state.selectedRoots.size} disco(s) selecionado(s)`;
    }

    elements.statFiles.textContent = formatNumber(st.totalFilesScanned);
    elements.statDirs.textContent = formatNumber(st.totalDirsScanned);

    elements.statBytes.textContent = formatBytes(st.totalBytesScanned);
    if (elements.statAllocatedBytes) {
      elements.statAllocatedBytes.textContent = formatBytes(st.totalAllocatedBytesScanned || st.totalBytesScanned);
    }
    if (elements.statCompressedCount) {
      elements.statCompressedCount.textContent = formatNumber(st.compressedFilesCount || 0);
    }
    if (elements.statCompressedSaved) {
      const saved = st.compressedSpaceSavedBytes || 0;
      if (saved > 0) {
        const ratio = st.compressionRatio ? ` (${st.compressionRatio.toFixed(2)}x)` : '';
        elements.statCompressedSaved.textContent = `+${formatBytes(saved)}${ratio}`;
        elements.statCompressedSaved.style.color = '#34d399';
      } else {
        elements.statCompressedSaved.textContent = '0 B';
        elements.statCompressedSaved.style.color = 'var(--text-muted)';
      }
    }
    elements.currentPathText.textContent = st.currentPath || 'Processando...';

    // QuickScan & AutoSave Badges
    if (st.isQuickScan) {
      if (elements.hudQuickScanBadge) elements.hudQuickScanBadge.classList.remove('hidden');
      if (elements.boxReusedFiles) elements.boxReusedFiles.style.display = 'flex';
      if (elements.boxNewModifiedFiles) elements.boxNewModifiedFiles.style.display = 'flex';
      if (elements.statReusedFiles) elements.statReusedFiles.textContent = formatNumber(st.reusedFilesCount || 0);
      if (elements.statNewModifiedFiles) elements.statNewModifiedFiles.textContent = formatNumber((st.modifiedFilesCount || 0) + (st.newFilesCount || 0));
    } else {
      if (elements.hudQuickScanBadge) elements.hudQuickScanBadge.classList.add('hidden');
      if (elements.boxReusedFiles) elements.boxReusedFiles.style.display = 'none';
      if (elements.boxNewModifiedFiles) elements.boxNewModifiedFiles.style.display = 'none';
    }

    if (st.lastAutoSaveTime > 0 && elements.hudAutoSaveBadge) {
      const d = new Date(st.lastAutoSaveTime * 1000);
      elements.hudAutoSaveBadge.textContent = `💾 Auto-Salvo (${d.toLocaleTimeString()})`;
      elements.hudAutoSaveBadge.classList.remove('hidden');
    }

    // Update Errors Badge
    if (st.errorsCount > 0) {
      elements.hudErrorsBadge.textContent = `⚠️ ${st.errorsCount} Itens Bloqueados / Ignorados`;
      elements.hudErrorsBadge.classList.remove('hidden');
    } else {
      elements.hudErrorsBadge.classList.add('hidden');
    }

    if (st.phase === 'loading_cache') {
      setGlobalOperationLock(
        true,
        'Carregando Snapshot de Cache para a Memória...',
        st.currentPath || 'Processando estrutura de cache...',
        st.progressPercent || 0,
        st.currentPath || ''
      );
    } else if (st.phase === 'phase1_metadata') {
      elements.hudPhaseBadge.textContent = st.isQuickScan ? 'Fase 1: Mapeamento e Verificação Incremental' : 'Fase 1: Mapeamento de Metadados em Memória';
      elements.hudPhaseBadge.style.background = 'rgba(56, 189, 248, 0.15)';
      elements.hudPhaseBadge.style.color = '#38bdf8';
      elements.statSpeed.textContent = `${Math.round(st.scanRateFilesPerSec || 0)} arq/s`;
      elements.progressBarFill.style.width = '40%';
      elements.progressPercentText.textContent = 'Fase 1 em andamento';
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = 'Fase 1: Mapeando...';
    } else if (st.phase === 'phase2_hashing') {
      elements.hudPhaseBadge.textContent = st.isQuickScan ? 'Fase 2: Hashes apenas de Novos / Modificados' : 'Fase 2: Cálculo Concorrente de Hashes';
      elements.hudPhaseBadge.style.background = 'rgba(168, 85, 247, 0.15)';
      elements.hudPhaseBadge.style.color = '#a855f7';
      elements.statSpeed.textContent = `${(st.hashRateMBPerSec || 0).toFixed(1)} MB/s`;
      const pct = Math.min(100, Math.max(0, st.progressPercent || 0));
      elements.progressBarFill.style.width = `${pct}%`;
      elements.progressPercentText.textContent = `${pct.toFixed(1)}% (${formatNumber(st.filesHashedCount)} / ${formatNumber(st.filesToHashCount)})`;
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = 'Fase 2: Hashes...';
    } else if (st.phase === 'completed' || st.phase === 'watching') {
      setGlobalOperationLock(false);
      elements.hudPhaseBadge.textContent = st.isWatching ? 'Sistema Conectado: Monitoramento em Tempo Real Ativo' : 'Varredura Concluída';
      elements.hudPhaseBadge.style.background = 'rgba(16, 185, 129, 0.15)';
      elements.hudPhaseBadge.style.color = '#10b981';
      elements.progressBarFill.style.width = '100%';
      elements.progressPercentText.textContent = '100%';
      elements.btnStartScan.classList.remove('hidden');
      if (elements.btnQuickScan) elements.btnQuickScan.classList.remove('hidden');
      elements.btnCancelScan.classList.add('hidden');
      elements.liveBadge.className = 'status-badge live';
      elements.liveStatusText.textContent = 'Live Monitoring';

      // Update duplicate counter badges
      if (st.duplicateGroupsCount > 0) {
        elements.dupCountBadge.textContent = st.duplicateGroupsCount;
        elements.dupCountBadge.classList.remove('hidden');
      }

      elements.dupTotalGroups.textContent = formatNumber(st.duplicateGroupsCount);
      elements.dupTotalFiles.textContent = formatNumber(st.duplicateFilesCount);
      elements.dupTotalWasted.textContent = formatBytes(st.duplicateWastedBytes);

      // Update folder duplicate badges
      if (st.duplicateFolderGroupsCount > 0) {
        elements.folderDupCountBadge.textContent = st.duplicateFolderGroupsCount;
        elements.folderDupCountBadge.classList.remove('hidden');
      }

      elements.dupFolderTotalGroups.textContent = formatNumber(st.duplicateFolderGroupsCount);
      elements.dupFolderTotalCount.textContent = formatNumber(st.duplicateFoldersCount);
      elements.dupFolderTotalWasted.textContent = formatBytes(st.duplicateFolderWastedBytes);

      // Auto-reload data if looking at other tabs
      if (state.currentTab === 'treeTab') loadTreeData(state.treePath);
      if (state.currentTab === 'duplicatesTab') loadDuplicates();
      if (state.currentTab === 'foldersTab') loadFolderDuplicates();
      if (state.currentTab === 'analyticsTab') loadAnalytics();
    }

    if (st.elapsedTimeSec > 0) {
      elements.statElapsed.textContent = `${Math.round(st.elapsedTimeSec)}s`;
    }

    // Render Active Worker Threads
    if (st.activeWorkers && st.activeWorkers.length > 0 && (st.phase === 'phase2_hashing' || st.phase === 'phase1_metadata')) {
      elements.activeWorkersSection.classList.remove('hidden');
      elements.activeWorkerCountText.textContent = `${st.activeWorkers.length} threads ativas`;
      elements.activeWorkersGrid.innerHTML = st.activeWorkers.map(w => {
        const pct = (w.percent || 0).toFixed(1);
        const fileName = w.path.split(/[\\/]/).pop() || w.path;
        return `
          <div class="worker-card">
            <div class="worker-card-header">
              <span>Thread #${w.workerId + 1}</span>
              <span>${w.totalSize > 0 ? formatBytes(w.totalSize) : 'Diretório'}</span>
            </div>
            <div class="worker-file-name" title="${w.path}">${fileName}</div>
            ${w.totalSize > 0 ? `
              <div class="worker-progress-bar">
                <div class="worker-progress-fill" style="width: ${pct}%"></div>
              </div>
              <div class="worker-stat-text">
                <span>${formatBytes(w.bytesDone)} / ${formatBytes(w.totalSize)}</span>
                <span>${pct}%</span>
              </div>
            ` : `
              <div class="worker-stat-text">
                <span class="truncate">${w.path}</span>
              </div>
            `}
          </div>
        `;
      }).join('');
    } else {
      elements.activeWorkersSection.classList.add('hidden');
    }

    // Render Recent Files Feed
    if (st.recentFiles && st.recentFiles.length > 0) {
      elements.recentFilesTableBody.innerHTML = st.recentFiles.map(rf => {
        const timeStr = new Date(rf.timestamp).toLocaleTimeString('pt-BR');
        const hashDisplay = rf.hash ? (rf.hash.length > 18 ? rf.hash.substring(0, 18) + '...' : rf.hash) : (rf.durationMs ? `${rf.durationMs}ms` : '-');
        const statusClass = rf.status || 'OK';

        return `
          <tr>
            <td>${timeStr}</td>
            <td><span class="status-pill status-${statusClass}">${statusClass}</span></td>
            <td><strong>${rf.size > 0 ? formatBytes(rf.size) : '-'}</strong></td>
            <td class="truncate" title="${rf.path}${rf.message ? ' - ' + rf.message : ''}">
              ${rf.path}
              ${rf.message ? `<small style="color:var(--accent-amber); display:block;">${rf.message}</small>` : ''}
            </td>
            <td><code style="font-size:0.75rem; color:var(--accent-cyan);">${hashDisplay}</code></td>
          </tr>
        `;
      }).reverse().join('');
    }
  }

  // ==========================================
  // TREE & CUSHION TREEMAP ENGINE (TreeSize Style)
  // ==========================================

  const FILE_TYPE_COLORS = {
    video: '#3b82f6',     // Blue
    audio: '#eab308',     // Yellow
    image: '#10b981',     // Green
    archive: '#f97316',   // Orange
    executable: '#f43f5e',// Rose
    document: '#06b6d4',  // Cyan
    code: '#8b5cf6',      // Purple
    folder: '#2563eb',    // Dark Blue
    other: '#64748b',     // Slate
  };

  const EXT_MAP = {
    // Video
    mp4: 'video', mkv: 'video', avi: 'video', mov: 'video', wmv: 'video', flv: 'video', webm: 'video', m4v: 'video', mpg: 'video', mpeg: 'video',
    // Audio
    mp3: 'audio', wav: 'audio', flac: 'audio', aac: 'audio', ogg: 'audio', m4a: 'audio', wma: 'audio', mid: 'audio', midi: 'audio',
    // Image
    jpg: 'image', jpeg: 'image', png: 'image', gif: 'image', webp: 'image', svg: 'image', bmp: 'image', psd: 'image', ai: 'image', ico: 'image', raw: 'image',
    // Archive / Compressed
    zip: 'archive', rar: 'archive', '7z': 'archive', tar: 'archive', gz: 'archive', bz2: 'archive', xz: 'archive', iso: 'archive', bin: 'archive', img: 'archive', vhd: 'archive', vhdx: 'archive', wim: 'archive',
    // Executable / Binary
    exe: 'executable', dll: 'executable', msi: 'executable', sys: 'executable', bat: 'executable', cmd: 'executable', ps1: 'executable', vbs: 'executable', com: 'executable',
    // Document
    pdf: 'document', doc: 'document', docx: 'document', xls: 'document', xlsx: 'document', ppt: 'document', pptx: 'document', txt: 'document', md: 'document', csv: 'document', rtf: 'document',
    // Code / Data
    js: 'code', ts: 'code', go: 'code', py: 'code', java: 'code', cpp: 'code', c: 'code', h: 'code', cs: 'code', php: 'code', html: 'code', css: 'code', json: 'code', xml: 'code', yaml: 'code', yml: 'code', sql: 'code', db: 'code', sqlite: 'code',
  };

  const LEVEL_COLORS = [
    '#1e3a8a', // Level 0 - Deep Blue (Root)
    '#0284c7', // Level 1 - Light Blue
    '#06b6d4', // Level 2 - Cyan
    '#0d9488', // Level 3 - Teal
    '#10b981', // Level 4 - Emerald
    '#eab308', // Level 5 - Yellow
    '#f97316', // Level 6 - Orange
    '#ef4444', // Level 7 - Red
    '#a855f7', // Level 8+ - Purple
  ];

  // Authentic WinDirStat Extension Color Palette (Faithful to KDirStat / WinDirStat)
  const WINDIRSTAT_EXT_COLORS = {
    // Virtual Disks & Images (WinDirStat Signature Royal Blue / Deep Indigo)
    vhdx: '#2563eb',
    vhd: '#1d4ed8',
    vmdk: '#1e40af',
    vdi: '#3b82f6',
    img: '#2563eb',
    iso: '#b45309',

    // Archives (WinDirStat Warm Amber / Golden Orange / Yellow)
    zip: '#d97706',
    rar: '#f59e0b',
    '7z': '#b45309',
    tar: '#d97706',
    gz: '#f59e0b',
    bz2: '#b45309',
    cab: '#ca8a04',
    wim: '#d97706',

    // System & Drivers (WinDirStat Teal / Cyan / Emerald)
    dll: '#06b6d4',
    sys: '#10b981',
    drv: '#0d9488',
    ocx: '#14b8a6',
    cpl: '#059669',

    // Game Assets & Packages (WinDirStat Magenta / Vibrant Purple)
    psarc: '#a855f7',
    pak: '#9333ea',
    bundle: '#c084fc',
    bin: '#7e22ce',
    dat: '#6b21a8',
    rpf: '#a855f7',

    // Executables (WinDirStat Cobalt / Azure)
    exe: '#3b82f6',
    msi: '#2563eb',
    bat: '#0284c7',
    cmd: '#0369a1',
    ps1: '#0284c7',

    // Images (WinDirStat Ruby Red / Crimson)
    jpg: '#ef4444',
    jpeg: '#dc2626',
    png: '#f43f5e',
    webp: '#e11d48',
    gif: '#fb7185',
    bmp: '#b91c1c',
    tiff: '#be123c',
    psd: '#e11d48',
    raw: '#be123c',

    // Videos (WinDirStat Violet / Purple)
    mp4: '#8b5cf6',
    mkv: '#7c3aed',
    avi: '#6d28d9',
    mov: '#a78bfa',
    wmv: '#5b21b6',

    // Audio (WinDirStat Lime / Olive)
    mp3: '#84cc16',
    flac: '#65a30d',
    wav: '#4d7c0f',
    m4a: '#a3e635',

    // Documents (WinDirStat Red/Sky/Green)
    pdf: '#dc2626',
    docx: '#0284c7',
    doc: '#0369a1',
    xlsx: '#16a34a',
    xls: '#15803d',
    pptx: '#ea580c',
    txt: '#64748b',

    // Code & Databases (WinDirStat Gold / Cyan)
    sqlite: '#0ea5e9',
    db: '#0284c7',
    sql: '#0369a1',
    js: '#eab308',
    ts: '#3b82f6',
    go: '#06b6d4',
    py: '#3b82f6',
  };

  function getExtensionColor(ext) {
    if (!ext) return '#64748b';
    const cleanExt = ext.replace(/^\./, '').toLowerCase();
    if (WINDIRSTAT_EXT_COLORS[cleanExt]) {
      return WINDIRSTAT_EXT_COLORS[cleanExt];
    }
    // High-contrast deterministic HSL color generator
    let hash = 0;
    for (let i = 0; i < cleanExt.length; i++) {
      hash = cleanExt.charCodeAt(i) + ((hash << 5) - hash);
    }
    const h = Math.abs(hash) % 360;
    return `hsl(${h}, 72%, 50%)`;
  }

  function getNodeColor(node, colorMode, level) {
    if (colorMode === 'depth') {
      return LEVEL_COLORS[level % LEVEL_COLORS.length];
    } else if (colorMode === 'age') {
      const nowSec = Date.now() / 1000;
      const modTime = node.modTime || nowSec;
      const ageDays = Math.max(0, Math.floor((nowSec - modTime) / 86400));
      if (ageDays < 30) return '#10b981'; // <1 month: green
      if (ageDays < 180) return '#06b6d4'; // 1-6 months: cyan
      if (ageDays < 365) return '#3b82f6'; // 6m-1y: blue
      if (ageDays < 730) return '#eab308'; // 1-2y: yellow
      if (ageDays < 1825) return '#f97316'; // 2-5y: orange
      return '#ef4444'; // >5y: red
    } else {
      // Extension Mode (WinDirStat 1:1 Fidelity)
      if (node.isFile) {
        const ext = (node.name || '').split('.').pop().toLowerCase();
        return getExtensionColor(ext);
      }
      return LEVEL_COLORS[level % LEVEL_COLORS.length];
    }
  }

  // Load Tree and Treemap Data from Backend
  async function loadTreeData(path = '') {
    state.treePath = path;
    state.treeTablePage = 1;
    renderBreadcrumbs(path);

    // Update Title & Subtitle
    if (elements.treemapCurrentTitle) {
      elements.treemapCurrentTitle.textContent = path ? `Gráfico: ${path}` : 'Gráfico da Estrutura (Todos os Discos)';
    }

    try {
      const depth = state.treemap.depth || 5;
      const url = `/api/tree?path=${encodeURIComponent(path)}&depth=${depth}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Não foi possível carregar o diretório.');
      const text = await res.text();
      let data = [];
      if (text && text.trim() !== '' && text.trim() !== 'null') {
        try {
          data = JSON.parse(text);
        } catch (e) {
          data = [];
        }
      }
      state.treeData = data;
      state.treemap.rawTree = data;

      renderTreeTable();
      resizeTreemapCanvas();
      renderTreemapLegend();
    } catch (err) {
      if (elements.treeTableBody) {
        elements.treeTableBody.innerHTML = `<tr><td colspan="7" class="empty-state">${err.message}</td></tr>`;
      }
    }
  }

  function treeGoUp() {
    if (!state.treePath) return;
    const clean = state.treePath.replace(/[\\/]+$/, '');
    const lastSlash = Math.max(clean.lastIndexOf('\\'), clean.lastIndexOf('/'));
    if (lastSlash <= 2) {
      // Reached drive root (e.g. C:\ or C:)
      if (clean.length > 3) {
        loadTreeData(clean.substring(0, 3));
      } else {
        loadTreeData('');
      }
    } else {
      loadTreeData(clean.substring(0, lastSlash));
    }
  }

  function renderBreadcrumbs(path) {
    if (!elements.treeBreadcrumbs) return;

    if (!path) {
      elements.treeBreadcrumbs.innerHTML = '<span class="breadcrumb-item active" data-path="">Meus Discos</span>';
      return;
    }

    const parts = path.split(/[\/\\]/).filter(Boolean);
    let html = '<span class="breadcrumb-item" data-path="">Meus Discos</span>';
    let accum = '';

    parts.forEach((p, idx) => {
      accum = idx === 0 ? p + '\\' : accum + '\\' + p;
      const isLast = idx === parts.length - 1;
      html += ` / <span class="breadcrumb-item ${isLast ? 'active' : ''}" data-path="${accum}">${p}</span>`;
    });

    elements.treeBreadcrumbs.innerHTML = html;

    elements.treeBreadcrumbs.querySelectorAll('.breadcrumb-item').forEach(b => {
      b.addEventListener('click', () => {
        const p = b.getAttribute('data-path');
        loadTreeData(p);
      });
    });
  }

  function renderTreeTable() {
    if (!elements.treeTableBody || !state.treeData) return;

    let items = [];
    let parentSize = 1;

    if (Array.isArray(state.treeData)) {
      // Roots view
      items = state.treeData.map(r => ({
        path: r.path,
        name: `💾 Unidade (${r.name || r.path})`,
        totalSize: r.totalSize || 0,
        totalAllocatedSize: r.totalAllocatedSize || r.totalSize || 0,
        fileCount: r.fileCount || 0,
        subDirCount: r.subDirCount || 0,
        modTime: r.modTime,
        createTime: r.createTime,
        isFile: false,
      }));
      parentSize = items.reduce((acc, cur) => acc + (cur.totalSize || 0), 0) || 1;
    } else {
      if (state.treeData.subDirs && Array.isArray(state.treeData.subDirs)) {
        for (let i = 0; i < state.treeData.subDirs.length; i++) {
          items.push(state.treeData.subDirs[i]);
        }
      }
      if (state.treeData.files && Array.isArray(state.treeData.files)) {
        for (let i = 0; i < state.treeData.files.length; i++) {
          const f = state.treeData.files[i];
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
        }
      }
    }

    const search = elements.treeSearchInput ? elements.treeSearchInput.value.toLowerCase().trim() : '';
    if (search) {
      items = items.filter(it => it.name.toLowerCase().includes(search));
    }

    if (items.length === 0) {
      elements.treeTableBody.innerHTML = '<tr><td colspan="8" class="empty-state">Nenhum item nesta pasta.</td></tr>';
      if (elements.treePaginationBar) elements.treePaginationBar.innerHTML = '';
      return;
    }

    const totalItems = items.length;
    const pageSize = state.treeTableLimit || 50;
    const totalPages = Math.max(1, Math.ceil(totalItems / pageSize));
    state.treeTablePage = Math.min(Math.max(1, state.treeTablePage || 1), totalPages);
    const startIdx = (state.treeTablePage - 1) * pageSize;
    const pagedItems = items.slice(startIdx, startIdx + pageSize);

    elements.treeTableBody.innerHTML = pagedItems.map(item => {
      const pct = Math.min(100, ((item.totalSize / parentSize) * 100)).toFixed(1);
      const isDir = !item.isFile;
      const allocated = item.totalAllocatedSize !== undefined ? item.totalAllocatedSize : (item.totalSize || 0);
      const hasCompression = item.isCompressed || (item.compressedFileCount > 0);
      const compressionBadge = item.isCompressed ?
        `<span class="badge" style="background: rgba(16, 185, 129, 0.15); color: #10b981; border: 1px solid rgba(16, 185, 129, 0.3); font-size: 10px; padding: 1px 4px; border-radius: 3px; margin-left: 4px;" title="Arquivo Comprimido no NTFS">📦 NTFS</span>` : '';

      return `
        <tr class="${isDir ? 'dir-row' : 'file-row'}" data-path="${item.path}" style="${isDir ? 'cursor: pointer;' : ''}">
          <td>
            <div class="tree-node-cell">
              <span class="tree-icon ${isDir ? '' : 'file-icon'}">
                ${isDir ? 
                  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"></path></svg>' :
                  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>'
                }
              </span>
              <span class="tree-node-name ${isDir ? 'clickable-dir' : ''}" data-path="${item.path}">${item.name}</span>
              ${item.isSymlink ? `
                <span class="badge" style="background: rgba(56, 189, 248, 0.15); color: #38bdf8; border: 1px solid rgba(56, 189, 248, 0.3); font-size: 10px; padding: 2px 6px; border-radius: 4px; margin-left: 6px;" title="Link Simbólico / Junção">🔗 Link</span>
                ${item.linkTarget ? `<span style="font-size: 11px; opacity: 0.65; margin-left: 4px;" title="${item.linkTarget}">➔ ${item.linkTarget}</span>` : ''}
              ` : ''}
            </div>
          </td>
          <td><strong>${formatBytes(item.totalSize)}</strong></td>
          <td>
            <span style="${allocated < item.totalSize ? 'color: #34d399; font-weight: 600;' : ''}">${formatBytes(allocated)}</span>
            ${compressionBadge}
          </td>
          <td>
            <div class="size-meter-cell">
              <div class="size-meter-bar">
                <div class="size-meter-fill" style="width: ${pct}%"></div>
              </div>
              <span>${pct}%</span>
            </div>
          </td>
          <td>${isDir ? formatNumber(item.fileCount) : '-'}</td>
          <td>${isDir ? formatNumber(item.subDirCount) : '-'}</td>
          <td>${formatDate(item.createTime)}</td>
          <td>${formatDate(item.modTime)}</td>
        </tr>
      `;
    }).join('');

    // Click handler to dive into folders (on entire row or name span)
    elements.treeTableBody.querySelectorAll('.dir-row').forEach(el => {
      el.addEventListener('click', (e) => {
        const p = el.getAttribute('data-path');
        loadTreeData(p);
      });
    });

    // Render tree pagination bar if items exceed page size
    if (elements.treePaginationBar) {
      if (totalItems > pageSize) {
        renderPaginationBar(
          elements.treePaginationBar,
          totalItems,
          state.treeTablePage,
          pageSize,
          (newPage) => {
            state.treeTablePage = newPage;
            renderTreeTable();
          },
          (newLimit) => {
            state.treeTableLimit = newLimit;
            state.treeTablePage = 1;
            renderTreeTable();
          }
        );
      } else {
        elements.treePaginationBar.innerHTML = '';
      }
    }
  }


  // Setup Treemap Canvas & Interactions
  function setupTreemap() {
    if (!elements.treemapCanvas || !elements.treemapContainer) return;

    window.addEventListener('resize', debounce(() => resizeTreemapCanvas(), 100));

    // Mouse Move (Hover Tooltip)
    elements.treemapCanvas.addEventListener('mousemove', (e) => {
      const rect = elements.treemapCanvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;

      // Find top-most / deepest matching leaf node
      let found = null;
      for (let i = state.treemap.layoutNodes.length - 1; i >= 0; i--) {
        const n = state.treemap.layoutNodes[i];
        if (x >= n.x && x <= n.x + n.w && y >= n.y && y <= n.y + n.h) {
          found = n;
          break;
        }
      }

      if (found !== state.treemap.hoveredNode) {
        state.treemap.hoveredNode = found;
        renderTreemapCanvas();
      }

      if (found && elements.treemapTooltip) {
        const parentTotal = state.treeData ? (state.treeData.totalSize || (Array.isArray(state.treeData) ? state.treeData.reduce((a, b) => a + (b.totalSize || 0), 0) : 1)) : 1;
        const pct = ((found.node.totalSize / parentTotal) * 100).toFixed(1);
        const isDir = !found.node.isFile;

        elements.treemapTooltip.innerHTML = `
          <div class="treemap-tooltip-title">${isDir ? '📁 ' : '📄 '}${found.node.name}</div>
          <div class="treemap-tooltip-metric"><span>Caminho:</span><strong class="truncate" style="max-width:200px;">${found.node.path}</strong></div>
          <div class="treemap-tooltip-metric"><span>Tamanho:</span><strong>${formatBytes(found.node.totalSize)} (${pct}%)</strong></div>
          ${isDir ? `
            <div class="treemap-tooltip-metric"><span>Arquivos:</span><strong>${formatNumber(found.node.fileCount)}</strong></div>
            <div class="treemap-tooltip-metric"><span>Subpastas:</span><strong>${formatNumber(found.node.subDirCount)}</strong></div>
          ` : ''}
          <div class="treemap-tooltip-metric"><span>Criado em:</span><span>${formatDate(found.node.createTime)}</span></div>
          <div class="treemap-tooltip-metric"><span>Modificado em:</span><span>${formatDate(found.node.modTime)}</span></div>
        `;

        elements.treemapTooltip.classList.remove('hidden');

        // Position tooltip near cursor but inside viewport
        let tipX = e.clientX - rect.left + 15;
        let tipY = e.clientY - rect.top + 15;
        const tipW = 280;
        const tipH = 150;

        if (tipX + tipW > rect.width) tipX = Math.max(10, rect.width - tipW - 10);
        if (tipY + tipH > rect.height) tipY = Math.max(10, rect.height - tipH - 10);

        elements.treemapTooltip.style.left = `${tipX}px`;
        elements.treemapTooltip.style.top = `${tipY}px`;
      } else if (elements.treemapTooltip) {
        elements.treemapTooltip.classList.add('hidden');
      }
    });

    // Mouse Leave
    elements.treemapCanvas.addEventListener('mouseleave', () => {
      state.treemap.hoveredNode = null;
      if (elements.treemapTooltip) elements.treemapTooltip.classList.add('hidden');
      renderTreemapCanvas();
    });

    // Click: Select Node & Highlight in Table
    elements.treemapCanvas.addEventListener('click', (e) => {
      if (elements.treemapContextMenu) elements.treemapContextMenu.classList.add('hidden');
      const rect = elements.treemapCanvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;

      let found = null;
      for (let i = state.treemap.layoutNodes.length - 1; i >= 0; i--) {
        const n = state.treemap.layoutNodes[i];
        if (x >= n.x && x <= n.x + n.w && y >= n.y && y <= n.y + n.h) {
          found = n;
          break;
        }
      }

      state.treemap.selectedNode = found;
      renderTreemapCanvas();

      if (found) {
        // Highlight in tree table and scroll to it
        const targetRow = elements.treeTableBody.querySelector(`tr[data-path="${CSS.escape(found.node.path)}"]`);
        if (targetRow) {
          elements.treeTableBody.querySelectorAll('tr').forEach(r => r.style.outline = 'none');
          targetRow.style.outline = '2px solid var(--accent-amber)';
          targetRow.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }
      }
    });

    // Double Click: Zoom in / Drill down into Folder
    elements.treemapCanvas.addEventListener('dblclick', (e) => {
      const rect = elements.treemapCanvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;

      let found = null;
      for (let i = state.treemap.layoutNodes.length - 1; i >= 0; i--) {
        const n = state.treemap.layoutNodes[i];
        if (x >= n.x && x <= n.x + n.w && y >= n.y && y <= n.y + n.h) {
          found = n;
          break;
        }
      }

      if (found) {
        if (!found.node.isFile) {
          loadTreeData(found.node.path);
        } else {
          // Double clicked file -> zoom into parent directory
          const p = found.node.path;
          const clean = p.replace(/[\\/]+$/, '');
          const lastSlash = Math.max(clean.lastIndexOf('\\'), clean.lastIndexOf('/'));
          if (lastSlash > 0) {
            loadTreeData(clean.substring(0, lastSlash));
          }
        }
      }
    });

    // Context Menu (Right Click)
    elements.treemapCanvas.addEventListener('contextmenu', (e) => {
      e.preventDefault();
      const rect = elements.treemapCanvas.getBoundingClientRect();
      const x = e.clientX - rect.left;
      const y = e.clientY - rect.top;

      let found = null;
      for (let i = state.treemap.layoutNodes.length - 1; i >= 0; i--) {
        const n = state.treemap.layoutNodes[i];
        if (x >= n.x && x <= n.x + n.w && y >= n.y && y <= n.y + n.h) {
          found = n;
          break;
        }
      }

      state.treemap.contextNode = found;

      if (found && elements.treemapContextMenu) {
        elements.treemapContextMenu.classList.remove('hidden');
        let posX = e.clientX - rect.left;
        let posY = e.clientY - rect.top;
        if (posX + 230 > rect.width) posX = Math.max(10, rect.width - 240);
        if (posY + 160 > rect.height) posY = Math.max(10, rect.height - 170);

        elements.treemapContextMenu.style.left = `${posX}px`;
        elements.treemapContextMenu.style.top = `${posY}px`;
      }
    });

    // Close Context Menu on Document Click
    document.addEventListener('click', (e) => {
      if (elements.treemapContextMenu && !elements.treemapContextMenu.contains(e.target)) {
        elements.treemapContextMenu.classList.add('hidden');
      }
    });

    // Context Menu Actions
    if (elements.ctxZoomIn) {
      elements.ctxZoomIn.addEventListener('click', () => {
        if (!state.treemap.contextNode) return;
        const n = state.treemap.contextNode.node;
        elements.treemapContextMenu.classList.add('hidden');
        if (!n.isFile) {
          loadTreeData(n.path);
        } else {
          const lastSlash = Math.max(n.path.lastIndexOf('\\'), n.path.lastIndexOf('/'));
          if (lastSlash > 0) loadTreeData(n.path.substring(0, lastSlash));
        }
      });
    }

    if (elements.ctxZoomOut) {
      elements.ctxZoomOut.addEventListener('click', () => {
        elements.treemapContextMenu.classList.add('hidden');
        treeGoUp();
      });
    }

    if (elements.ctxCopyPath) {
      elements.ctxCopyPath.addEventListener('click', () => {
        if (!state.treemap.contextNode) return;
        navigator.clipboard.writeText(state.treemap.contextNode.node.path);
        showToast('Caminho copiado para a área de transferência!', 'success');
        elements.treemapContextMenu.classList.add('hidden');
      });
    }

    if (elements.ctxRecycle) {
      elements.ctxRecycle.addEventListener('click', async () => {
        if (!state.treemap.contextNode) return;
        const n = state.treemap.contextNode.node;
        elements.treemapContextMenu.classList.add('hidden');

        const confirmed = confirm(
          `Tem certeza que deseja enviar "${n.name}" (${formatBytes(n.totalSize)}) para a LIXEIRA DO WINDOWS?`
        );
        if (!confirmed) return;

        try {
          const res = await fetch('/api/files/recycle', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ paths: [n.path] }),
          });
          if (!res.ok) throw new Error(await res.text());
          const result = await res.json();
          showToast(`Sucesso! ${result.successCount} item(ns) enviados para a Lixeira.`, 'success');
          loadTreeData(state.treePath);
        } catch (err) {
          showToast('Erro ao enviar para a lixeira: ' + err.message, 'danger');
        }
      });
    }

    // Splitter Dragging Interaction
    if (elements.treeSplitter && elements.treeSplitLayout) {
      let isDragging = false;

      // Restore saved split ratio
      const savedRatio = localStorage.getItem('scanfile_split_left');
      if (savedRatio) {
        const left = parseFloat(savedRatio);
        if (left >= 0.2 && left <= 0.8) {
          elements.treeSplitLayout.style.setProperty('--tree-left-flex', left.toFixed(3));
          elements.treeSplitLayout.style.setProperty('--tree-right-flex', (1 - left).toFixed(3));
        }
      }

      elements.treeSplitter.addEventListener('pointerdown', (e) => {
        isDragging = true;
        elements.treeSplitter.classList.add('dragging');
        elements.treeSplitter.setPointerCapture(e.pointerId);
        document.body.style.userSelect = 'none';
        document.body.style.cursor = 'col-resize';
      });

      elements.treeSplitter.addEventListener('pointermove', (e) => {
        if (!isDragging) return;
        const rect = elements.treeSplitLayout.getBoundingClientRect();
        if (rect.width <= 0) return;
        const offsetX = e.clientX - rect.left;
        let leftRatio = offsetX / rect.width;
        leftRatio = Math.max(0.2, Math.min(0.8, leftRatio));
        const rightRatio = 1 - leftRatio;

        elements.treeSplitLayout.style.setProperty('--tree-left-flex', leftRatio.toFixed(3));
        elements.treeSplitLayout.style.setProperty('--tree-right-flex', rightRatio.toFixed(3));
        localStorage.setItem('scanfile_split_left', leftRatio.toFixed(3));
        resizeTreemapCanvas();
      });

      const stopDrag = (e) => {
        if (!isDragging) return;
        isDragging = false;
        elements.treeSplitter.classList.remove('dragging');
        try { elements.treeSplitter.releasePointerCapture(e.pointerId); } catch (_) {}
        document.body.style.userSelect = '';
        document.body.style.cursor = '';
        resizeTreemapCanvas();
      };

      elements.treeSplitter.addEventListener('pointerup', stopDrag);
      elements.treeSplitter.addEventListener('pointercancel', stopDrag);
    }

    // Max Height / Compact Fullscreen Toggle
    if (elements.btnTreeMaxHeight) {
      const updateMaxHeightBtn = (isMax) => {
        elements.btnTreeMaxHeight.innerHTML = isMax ? '🗗 Restaurar Altura' : '⛶ Tela Máxima';
        elements.btnTreeMaxHeight.title = isMax ? 'Restaurar layout padrão' : 'Expandir altura máxima na tela';
        if (isMax) {
          elements.btnTreeMaxHeight.classList.add('active');
        } else {
          elements.btnTreeMaxHeight.classList.remove('active');
        }
      };

      const savedMaxHeight = localStorage.getItem('scanfile_max_height') === 'true';
      if (savedMaxHeight) {
        document.body.classList.add('ultra-height-mode');
        updateMaxHeightBtn(true);
      }

      elements.btnTreeMaxHeight.addEventListener('click', () => {
        const isMax = document.body.classList.toggle('ultra-height-mode');
        localStorage.setItem('scanfile_max_height', isMax);
        updateMaxHeightBtn(isMax);
        setTimeout(() => resizeTreemapCanvas(), 60);
      });
    }
  }

  // Resize Treemap Canvas with DPI Scaling
  function resizeTreemapCanvas() {
    if (!elements.treemapCanvas || !elements.treemapContainer) return;

    const rect = elements.treemapContainer.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    const width = Math.floor(rect.width);
    const height = Math.floor(rect.height);

    if (width <= 0 || height <= 0) return;

    elements.treemapCanvas.width = width * dpr;
    elements.treemapCanvas.height = height * dpr;
    elements.treemapCanvas.style.width = `${width}px`;
    elements.treemapCanvas.style.height = `${height}px`;

    const ctx = elements.treemapCanvas.getContext('2d');
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    // Compute Layout
    if (state.treemap.rawTree) {
      const maxDepth = state.treemap.depth || 5;
      let rootContainer = null;

      if (Array.isArray(state.treemap.rawTree)) {
        // Multi-root wrapper
        const total = state.treemap.rawTree.reduce((a, b) => a + (b.totalSize || 0), 0);
        rootContainer = {
          name: 'Meus Discos',
          path: '',
          totalSize: total,
          subDirs: state.treemap.rawTree,
          files: [],
          isFile: false,
        };
      } else {
        rootContainer = state.treemap.rawTree;
      }

      state.treemap.layoutNodes = computeSquarifiedLayout(rootContainer, 0, 0, width, height, 0, maxDepth);
    } else {
      state.treemap.layoutNodes = [];
    }

    renderTreemapCanvas();
  }

  // Squarified Treemap Layout Computation (Bruls, Huizing, van Wijk algorithm) - Stack-Safe & Heap-Capped
  function computeSquarifiedLayout(container, x, y, width, height, level, maxDepth, results = []) {
    if (width <= 0 || height <= 0 || !container || results.length >= 2000) return results;

    const effectiveMaxDepth = Math.min(maxDepth || 4, 8);

    // Collect children: subdirectories and files safely without array spreads
    const children = [];
    if (container.subDirs && Array.isArray(container.subDirs)) {
      for (let i = 0; i < container.subDirs.length; i++) {
        const d = container.subDirs[i];
        if (d && (d.totalSize || 0) > 0) {
          children.push({
            name: d.name,
            path: d.path,
            totalSize: d.totalSize || 0,
            totalAllocatedSize: d.totalAllocatedSize || d.totalSize || 0,
            fileCount: d.fileCount || 0,
            subDirCount: d.subDirCount || 0,
            modTime: d.modTime,
            createTime: d.createTime,
            isFile: false,
            subDirs: d.subDirs,
            files: d.files,
          });
        }
      }
    }
    if (container.files && Array.isArray(container.files)) {
      for (let i = 0; i < container.files.length; i++) {
        const f = container.files[i];
        const sz = f ? (f.size || f.totalSize || 0) : 0;
        if (sz > 0) {
          children.push({
            name: f.name,
            path: f.path,
            totalSize: sz,
            totalAllocatedSize: f.allocatedSize || sz,
            fileCount: 1,
            subDirCount: 0,
            modTime: f.modTime,
            createTime: f.createTime,
            isFile: true,
          });
        }
      }
    }

    children.sort((a, b) => b.totalSize - a.totalSize);
    const totalChildSize = children.reduce((acc, c) => acc + c.totalSize, 0);

    if (children.length === 0 || level >= effectiveMaxDepth || totalChildSize <= 0) {
      results.push({
        x, y, w: width, h: height,
        node: container,
        level,
        isLeaf: true,
      });
      return results;
    }

    // Squarify all children into the rectangle [x, y, width, height]
    const rects = squarify(children, totalChildSize, x, y, width, height);

    for (let i = 0; i < rects.length; i++) {
      const item = rects[i];
      const hasSubChildren = (item.node.subDirs && item.node.subDirs.length > 0) || 
                             (item.node.files && item.node.files.length > 0);
      const isLeafChild = item.node.isFile || level + 1 >= effectiveMaxDepth || !hasSubChildren;

      if (isLeafChild) {
        results.push({
          x: item.x,
          y: item.y,
          w: item.w,
          h: item.h,
          node: item.node,
          level: level + 1,
          isLeaf: true,
        });
      } else {
        // Recurse into subdirectory passing results accumulator directly (Stack-Safe)
        computeSquarifiedLayout(item.node, item.x, item.y, item.w, item.h, level + 1, effectiveMaxDepth, results);
      }
    }

    return results;
  }

  // Exact Mathematical Squarified Treemap (100% Canvas Area Filling, Zero Shrinking)
  function squarify(children, totalSize, x, y, width, height) {
    if (children.length === 0 || width <= 0 || height <= 0 || totalSize <= 0) return [];

    const rects = [];
    let remaining = children.slice();
    let remSize = totalSize;
    let rx = x;
    let ry = y;
    let rw = width;
    let rh = height;

    while (remaining.length > 0) {
      if (rw <= 0 || rh <= 0 || remSize <= 0) break;

      const isHorizontal = rw >= rh;
      const side = isHorizontal ? rh : rw;

      let row = [remaining[0]];
      let bestWorst = worstAspect(row, side, remSize, isHorizontal ? rw : rh);
      let i = 1;

      while (i < remaining.length) {
        const testRow = row.concat([remaining[i]]);
        const testWorst = worstAspect(testRow, side, remSize, isHorizontal ? rw : rh);
        if (testWorst <= bestWorst) {
          bestWorst = testWorst;
          row = testRow;
          i++;
        } else {
          break;
        }
      }

      const rowSum = row.reduce((acc, c) => acc + c.totalSize, 0);
      // Strip thickness along the perpendicular dimension
      const stripThickness = remSize > 0 ? (rowSum / remSize) * (isHorizontal ? rw : rh) : 0;

      let offset = 0;
      for (let j = 0; j < row.length; j++) {
        const item = row[j];
        const itemLen = rowSum > 0 ? (item.totalSize / rowSum) * side : 0;
        if (isHorizontal) {
          rects.push({
            x: rx,
            y: ry + offset,
            w: stripThickness,
            h: itemLen,
            node: item,
          });
        } else {
          rects.push({
            x: rx + offset,
            y: ry,
            w: itemLen,
            h: stripThickness,
            node: item,
          });
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
      remaining = remaining.slice(row.length);
    }

    return rects;
  }

  function worstAspect(row, side, totalSize, totalLength) {
    if (row.length === 0 || side <= 0 || totalSize <= 0 || totalLength <= 0) return Infinity;
    const rowSum = row.reduce((acc, c) => acc + c.totalSize, 0);
    const rowThickness = (rowSum / totalSize) * totalLength;
    if (rowThickness <= 0) return Infinity;

    let maxRatio = 0;
    for (let i = 0; i < row.length; i++) {
      const item = row[i];
      const itemLen = (item.totalSize / rowSum) * side;
      if (itemLen <= 0) return Infinity;
      const ratio = Math.max(itemLen / rowThickness, rowThickness / itemLen);
      if (ratio > maxRatio) maxRatio = ratio;
    }
    return maxRatio;
  }

  // Render Treemap on Canvas with Authentic WinDirStat Cushion Shading
  function renderTreemapCanvas() {
    if (!elements.treemapCanvas) return;
    try {
      const ctx = elements.treemapCanvas.getContext('2d');
      const width = parseFloat(elements.treemapCanvas.style.width) || elements.treemapCanvas.width;
      const height = parseFloat(elements.treemapCanvas.style.height) || elements.treemapCanvas.height;

      // Clear
      ctx.clearRect(0, 0, width, height);

      if (!state.treemap.layoutNodes || state.treemap.layoutNodes.length === 0) {
        ctx.fillStyle = '#64748b';
        ctx.font = '14px Inter, sans-serif';
        ctx.textAlign = 'center';
        ctx.fillText('Nenhum dado para exibir no Treemap. Inicie uma varredura para visualizar os blocos.', width / 2, height / 2);
        ctx.textAlign = 'left';
        return;
      }

      const colorMode = state.treemap.colorMode || 'extension';
      let leaves = state.treemap.layoutNodes.filter(n => n.isLeaf && n.w > 0.5 && n.h > 0.5 && Number.isFinite(n.x) && Number.isFinite(n.y));

      // Guard against GPU memory overload: sort by area descending and cap to top 1,500 blocks
      if (leaves.length > 1500) {
        leaves.sort((a, b) => (b.w * b.h) - (a.w * a.h));
        leaves = leaves.slice(0, 1500);
      }

      // Draw leaf cushions (True WinDirStat 3D Specular Pillows)
      for (let i = 0; i < leaves.length; i++) {
        const n = leaves[i];
        const isHovered = state.treemap.hoveredNode && state.treemap.hoveredNode.node && state.treemap.hoveredNode.node.path === n.node.path;
        const isSelected = state.treemap.selectedNode && state.treemap.selectedNode.node && state.treemap.selectedNode.node.path === n.node.path;
        const baseColor = getNodeColor(n.node, colorMode, n.level);

        const label = n.node.name;
        const sublabel = formatBytes(n.node.totalSize);

        drawCushionRect(ctx, n.x, n.y, n.w, n.h, baseColor, isHovered, isSelected, label, sublabel, n.isLeaf, n.level);
      }
    } catch (err) {
      console.warn('[Treemap Safety Guard] Erro ao renderizar canvas:', err);
    }
  }

  // Draw Authentic WinDirStat 3D Glossy Cushion Shading (GPU-Safe & Crash-Proof)
  function drawCushionRect(ctx, x, y, w, h, baseColor, isHovered, isSelected, label, sublabel, isLeaf, level) {
    if (w <= 0 || h <= 0) return;

    // 1. Base Solid Color Fill
    ctx.fillStyle = baseColor;
    ctx.fillRect(x, y, w, h);

    // 2. WinDirStat Specular Cushion Highlight (Spherical / Diagonal Gloss Lighting)
    // CRITICAL: Only create expensive CanvasRadialGradient on visible blocks (w >= 14 && h >= 14) to prevent STATUS_BREAKPOINT crashes in Chromium
    if (w >= 14 && h >= 14) {
      // Off-center specular highlight point towards top-left
      const cx = x + w * 0.35;
      const cy = y + h * 0.35;
      const radius = Math.max(w, h) * 0.9;

      const radialGrad = ctx.createRadialGradient(cx, cy, 0, cx, cy, radius);
      radialGrad.addColorStop(0, 'rgba(255, 255, 255, 0.45)');  // Glossy reflection bulb
      radialGrad.addColorStop(0.35, 'rgba(255, 255, 255, 0.10)');
      radialGrad.addColorStop(0.7, 'rgba(0, 0, 0, 0.25)');
      radialGrad.addColorStop(1, 'rgba(0, 0, 0, 0.65)');       // Soft edge shadow

      ctx.fillStyle = radialGrad;
      ctx.fillRect(x, y, w, h);

      // Inner Light Bevel (Top & Left edge highlight)
      if (w > 20 && h > 20) {
        ctx.fillStyle = 'rgba(255, 255, 255, 0.32)';
        ctx.fillRect(x, y, w, 1);
        ctx.fillRect(x, y, 1, h);

        // Inner Shadow Bevel (Bottom & Right edge shadow)
        ctx.fillStyle = 'rgba(0, 0, 0, 0.45)';
        ctx.fillRect(x, y + h - 1, w, 1);
        ctx.fillRect(x + w - 1, y, 1, h);
      }
    } else if (w >= 4 && h >= 4) {
      // Fast lightweight specular highlight for medium blocks without gradient allocation
      ctx.fillStyle = 'rgba(255, 255, 255, 0.18)';
      ctx.fillRect(x, y, w, 1);
      ctx.fillRect(x, y, 1, h);
      ctx.fillStyle = 'rgba(0, 0, 0, 0.35)';
      ctx.fillRect(x, y + h - 1, w, 1);
      ctx.fillRect(x + w - 1, y, 1, h);
    }

    // 3. Outer Border / Pillow Seam
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

    // 4. Text Label (Clipped cleanly inside block)
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

  // Render Treemap Legend Bar (Dynamic File Extension Stats - WinDirStat Style, Iterative & Stack-Safe)
  function renderTreemapLegend() {
    if (!elements.treemapLegendBar) return;
    const colorMode = state.treemap.colorMode || 'extension';

    if (colorMode === 'depth') {
      elements.treemapLegendBar.innerHTML = LEVEL_COLORS.map((c, idx) => `
        <div class="legend-chip">
          <span class="legend-color-dot" style="background:${c}"></span>
          <span>Nível ${idx}</span>
        </div>
      `).join('');
    } else if (colorMode === 'age') {
      const ageLabels = [
        { color: '#10b981', label: '< 1 Mês' },
        { color: '#06b6d4', label: '1 a 6 Meses' },
        { color: '#3b82f6', label: '6m a 1 Ano' },
        { color: '#eab308', label: '1 a 2 Anos' },
        { color: '#f97316', label: '2 a 5 Anos' },
        { color: '#ef4444', label: '> 5 Anos' },
      ];
      elements.treemapLegendBar.innerHTML = ageLabels.map(a => `
        <div class="legend-chip">
          <span class="legend-color-dot" style="background:${a.color}"></span>
          <span>${a.label}</span>
        </div>
      `).join('');
    } else {
      // Dynamic Extension Stats from Current Tree (Iterative Queue Traversal - Stack-Safe)
      const extStats = {};
      let totalTreeBytes = 0;
      const queue = [];

      if (Array.isArray(state.treemap.rawTree)) {
        for (let i = 0; i < state.treemap.rawTree.length; i++) {
          if (state.treemap.rawTree[i]) queue.push(state.treemap.rawTree[i]);
        }
      } else if (state.treemap.rawTree) {
        queue.push(state.treemap.rawTree);
      }

      while (queue.length > 0) {
        const node = queue.pop();
        if (!node) continue;
        if (node.files && Array.isArray(node.files)) {
          for (let i = 0; i < node.files.length; i++) {
            const f = node.files[i];
            if (!f) continue;
            const sz = f.size || f.totalSize || 0;
            if (sz <= 0) continue;
            totalTreeBytes += sz;
            const ext = (f.name || '').split('.').pop().toLowerCase();
            if (!extStats[ext]) {
              extStats[ext] = { ext, totalBytes: 0, count: 0, color: getExtensionColor(ext) };
            }
            extStats[ext].totalBytes += sz;
            extStats[ext].count++;
          }
        }
        if (node.subDirs && Array.isArray(node.subDirs)) {
          for (let i = 0; i < node.subDirs.length; i++) {
            if (node.subDirs[i]) queue.push(node.subDirs[i]);
          }
        }
      }

      const sortedExts = Object.values(extStats).sort((a, b) => b.totalBytes - a.totalBytes);

      if (sortedExts.length > 0) {
        const topExts = sortedExts.slice(0, 10);
        elements.treemapLegendBar.innerHTML = topExts.map(t => {
          const pct = totalTreeBytes > 0 ? ((t.totalBytes / totalTreeBytes) * 100).toFixed(1) : '0.0';
          return `
            <div class="legend-chip" title="${formatNumber(t.count)} arquivos .${t.ext}">
              <span class="legend-color-dot" style="background:${t.color}"></span>
              <strong>.${t.ext}</strong>
              <span style="opacity:0.85;">${formatBytes(t.totalBytes)} (${pct}%)</span>
            </div>
          `;
        }).join('');
      } else {
        // Common WinDirStat file extensions
        const defaultExts = [
          { ext: 'vhdx', color: getExtensionColor('vhdx'), label: '.vhdx' },
          { ext: 'zip', color: getExtensionColor('zip'), label: '.zip / .rar' },
          { ext: 'sys', color: getExtensionColor('sys'), label: '.sys / .dll' },
          { ext: 'exe', color: getExtensionColor('exe'), label: '.exe / .msi' },
          { ext: 'psarc', color: getExtensionColor('psarc'), label: '.psarc / .pak' },
          { ext: 'jpg', color: getExtensionColor('jpg'), label: '.jpg / .png' },
          { ext: 'mp4', color: getExtensionColor('mp4'), label: '.mp4 / .mkv' },
          { ext: 'iso', color: getExtensionColor('iso'), label: '.iso / .img' },
        ];
        elements.treemapLegendBar.innerHTML = defaultExts.map(t => `
          <div class="legend-chip">
            <span class="legend-color-dot" style="background:${t.color}"></span>
            <span>${t.label}</span>
          </div>
        `).join('');
      }
    }
  }

  // Load Duplicate Groups
  async function loadDuplicates(page = 1) {
    if (state.isLoadingDups) return;

    state.dupPage = Math.max(1, page);
    const limit = state.dupLimit || 50;
    const offset = (state.dupPage - 1) * limit;

    const sortBy = elements.dupSortBy ? elements.dupSortBy.value : 'size_desc';
    const minSize = elements.dupMinSize ? elements.dupMinSize.value : 0;
    const search = elements.dupSearch ? elements.dupSearch.value.trim() : '';

    try {
      state.isLoadingDups = true;
      if (elements.duplicatesContainer) {
        elements.duplicatesContainer.innerHTML = '<div class="loading-state">Carregando duplicados por hash...</div>';
      }

      const url = `/api/duplicates?sortBy=${sortBy}&minSize=${minSize}&search=${encodeURIComponent(search)}&limit=${limit}&offset=${offset}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Falha ao carregar duplicados');
      const data = await res.json();
      state.duplicatesData = data;
      state.dupGroups = data.groups || [];
      state.dupTotalGroups = data.totalGroups || 0;

      renderDuplicates(data);
    } catch (err) {
      if (elements.duplicatesContainer) {
        elements.duplicatesContainer.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
      }
    } finally {
      state.isLoadingDups = false;
    }
  }

  // Render Duplicates Cards
  function renderDuplicates(data) {
    if (!elements.duplicatesContainer) return;

    if (!data.groups || data.groups.length === 0) {
      elements.duplicatesContainer.innerHTML = '<div class="empty-state">Nenhum arquivo duplicado encontrado com os filtros atuais.</div>';
      if (elements.dupTotalGroups) elements.dupTotalGroups.textContent = '0';
      if (elements.dupTotalFiles) elements.dupTotalFiles.textContent = '0';
      if (elements.dupTotalWasted) elements.dupTotalWasted.textContent = '0 B';
      if (elements.dupPaginationBar) elements.dupPaginationBar.innerHTML = '';
      return;
    }

    if (elements.dupTotalGroups) elements.dupTotalGroups.textContent = formatNumber(data.totalGroups || 0);
    if (elements.dupTotalFiles) elements.dupTotalFiles.textContent = formatNumber(data.totalFiles || 0);
    if (elements.dupTotalWasted) elements.dupTotalWasted.textContent = formatBytes(data.wastedBytes || 0);
    elements.duplicatesContainer.innerHTML = '';

    const fragment = document.createDocumentFragment();

    data.groups.forEach(grp => {
      const hashShort = (grp.hash && grp.hash.length > 22) ? grp.hash.substring(0, 22) + '...' : (grp.hash || 'hash');

      const card = document.createElement('div');
      card.className = 'dup-group-card';
      card.setAttribute('data-group-id', grp.id);

      card.innerHTML = `
        <div class="dup-group-header">
          <div class="dup-group-left">
            <span class="dup-hash-pill" title="${grp.hash}">${hashShort}</span>
            <span class="dup-size-pill">${formatBytes(grp.fileSize)} cada</span>
            <span class="dup-wasted-badge">Desperdiçado: ${formatBytes(grp.wastedBytes)} (${grp.fileCount} cópias)</span>
          </div>
          <div class="dup-group-actions">
            <button class="btn btn-secondary btn-sm btn-group-keep-newest" data-group-id="${grp.id}">⭐ Manter +Recente</button>
          </div>
        </div>

        <div class="dup-file-list">
          ${(grp.files || []).map((file, idx) => {
            const isMarked = state.selectedFilesForDelete.has(file.path);

            return `
              <div class="dup-file-row ${isMarked ? 'marked-for-delete' : ''}">
                <div class="dup-file-left">
                  <input type="checkbox" class="dup-file-checkbox" 
                         data-path="${file.path}" 
                         data-size="${file.size}" 
                         ${isMarked ? 'checked' : ''}>
                  <span class="dup-file-path truncate" title="${file.path}">${file.path}</span>
                </div>
                <div class="dup-file-date">
                  ${formatDate(file.modTime)}
                </div>
              </div>
            `;
          }).join('')}
        </div>
      `;

      // Attach event listeners for this card
      card.querySelectorAll('.dup-file-checkbox').forEach(cb => {
        cb.addEventListener('change', (e) => {
          const path = cb.getAttribute('data-path');
          const size = parseInt(cb.getAttribute('data-size'), 10);
          if (cb.checked) {
            state.selectedFilesForDelete.set(path, size);
          } else {
            state.selectedFilesForDelete.delete(path);
          }
          updateSelectionSummary();
          cb.closest('.dup-file-row').classList.toggle('marked-for-delete', cb.checked);
        });
      });

      card.querySelectorAll('.btn-group-keep-newest').forEach(btn => {
        btn.addEventListener('click', () => {
          const gId = btn.getAttribute('data-group-id');
          const g = data.groups.find(x => x.id === gId) || grp;
          if (!g || !g.files || g.files.length < 2) return;

          const newest = g.files[g.files.length - 1];
          g.files.forEach(f => {
            if (f.path !== newest.path) {
              state.selectedFilesForDelete.set(f.path, f.size);
            } else {
              state.selectedFilesForDelete.delete(f.path);
            }
          });
          card.querySelectorAll('.dup-file-row').forEach(row => {
            const cb = row.querySelector('.dup-file-checkbox');
            if (cb) {
              const p = cb.getAttribute('data-path');
              const isMarked = state.selectedFilesForDelete.has(p);
              cb.checked = isMarked;
              row.classList.toggle('marked-for-delete', isMarked);
            }
          });
          updateSelectionSummary();
        });
      });

      fragment.appendChild(card);
    });

    elements.duplicatesContainer.appendChild(fragment);

    // Universal pagination bar
    renderPaginationBar(
      elements.dupPaginationBar,
      data.totalGroups || 0,
      state.dupPage,
      state.dupLimit,
      (newPage) => loadDuplicates(newPage),
      (newLimit) => {
        state.dupLimit = newLimit;
        loadDuplicates(1);
      }
    );

    updateSelectionSummary();
  }

  // Strategy Auto-Selection
  function selectDuplicatesByStrategy(strategy) {
    if (!state.duplicatesData || !state.duplicatesData.groups) return;

    state.selectedFilesForDelete.clear();

    state.duplicatesData.groups.forEach(grp => {
      if (grp.files.length < 2) return;

      if (strategy === 'keep_newest') {
        // Files sorted by modTime ascending: index 0 is oldest, last is newest
        const keepFile = grp.files[grp.files.length - 1];
        grp.files.forEach(f => {
          if (f.path !== keepFile.path) {
            state.selectedFilesForDelete.set(f.path, f.size);
          }
        });
      } else if (strategy === 'keep_oldest') {
        const keepFile = grp.files[0];
        grp.files.forEach(f => {
          if (f.path !== keepFile.path) {
            state.selectedFilesForDelete.set(f.path, f.size);
          }
        });
      }
    });

    renderDuplicates(state.duplicatesData);
    updateSelectionSummary();
    showToast(`Seleção aplicada com a regra: ${strategy === 'keep_newest' ? 'Manter +Recente' : 'Manter +Antigo'}`, 'info');
  }

  function clearSelection() {
    state.selectedFilesForDelete.clear();
    renderDuplicates(state.duplicatesData);
    updateSelectionSummary();
  }

  function updateSelectionSummary() {
    const count = state.selectedFilesForDelete.size;
    let totalBytes = 0;
    state.selectedFilesForDelete.forEach(sz => totalBytes += sz);

    elements.dupSelectedCount.textContent = `${count} (${formatBytes(totalBytes)})`;
    elements.btnRecycleSelected.disabled = count === 0;
    elements.btnRecycleSelected.innerHTML = `
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>
      Mover ${count} Marcados para a Lixeira (${formatBytes(totalBytes)})
    `;
  }

  // Recycle Selected Files via Windows Shell API
  async function recycleSelectedFiles() {
    const files = Array.from(state.selectedFilesForDelete.keys());
    if (files.length === 0) return;

    let totalBytes = 0;
    state.selectedFilesForDelete.forEach(sz => totalBytes += sz);

    const confirmed = confirm(
      `Tem certeza que deseja enviar ${files.length} arquivo(s) (${formatBytes(totalBytes)}) para a LIXEIRA DO WINDOWS?\n\n` +
      `Os arquivos poderão ser restaurados através da Lixeira nativa do Windows caso necessário.`
    );

    if (!confirmed) return;

    try {
      const res = await fetch('/api/files/recycle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths: files }),
      });

      if (!res.ok) throw new Error(await res.text());
      const result = await res.json();

      showToast(`Sucesso! ${result.successCount} arquivo(s) enviados para a Lixeira do Windows. (${formatBytes(result.freedBytes)} liberados)`, 'success');
      state.selectedFilesForDelete.clear();
      loadDuplicates();
    } catch (err) {
      showToast('Erro ao reciclar arquivos: ' + err.message, 'danger');
    }
  }

  // Analytics tab
  async function loadAnalytics() {
    try {
      const res = await fetch('/api/stats/extensions');
      if (!res.ok) throw new Error('Falha ao carregar estatísticas');
      const stats = await res.json();

      if (!stats || stats.length === 0) {
        elements.extensionsTableBody.innerHTML = '<tr><td colspan="5" class="empty-state">Sem dados para exibir.</td></tr>';
        return;
      }

      elements.extensionsTableBody.innerHTML = stats.map(st => {
        const pct = (st.percentage || 0).toFixed(1);
        return `
          <tr>
            <td><strong>${st.extension}</strong></td>
            <td>${formatBytes(st.totalBytes)}</td>
            <td>${formatNumber(st.fileCount)} arquivos</td>
            <td>${pct}%</td>
            <td>
              <div class="size-meter-cell">
                <div class="size-meter-bar">
                  <div class="size-meter-fill" style="width: ${pct}%"></div>
                </div>
              </div>
            </td>
          </tr>
        `;
      }).join('');
    } catch (err) {
      elements.extensionsTableBody.innerHTML = `<tr><td colspan="5" class="empty-state">${err.message}</td></tr>`;
    }
  }

  // Cache Management Modals
  function setupCacheModals() {
    // Generic Modal Close Buttons
    document.querySelectorAll('[data-close-modal]').forEach(btn => {
      btn.addEventListener('click', () => {
        const modalId = btn.getAttribute('data-close-modal');
        const m = document.getElementById(modalId);
        if (m) m.classList.add('hidden');
      });
    });

    // Close on clicking backdrop
    document.querySelectorAll('.modal-overlay').forEach(modal => {
      modal.addEventListener('click', (e) => {
        if (e.target === modal) modal.classList.add('hidden');
      });
    });

    // Open Save Cache Modal
    if (elements.btnOpenSaveCacheModal) {
      elements.btnOpenSaveCacheModal.addEventListener('click', () => {
        const nowStr = new Date().toISOString().replace(/[:.]/g, '-').slice(0, 19);
        elements.saveCacheFileName.value = `scanfile_cache_${nowStr}.scanfile.gz`;
        elements.saveCacheModal.classList.remove('hidden');
      });
    }

    // Confirm Save Cache
    if (elements.btnConfirmSaveCache) {
      elements.btnConfirmSaveCache.addEventListener('click', async () => {
        const fileName = elements.saveCacheFileName.value.trim();
        try {
          elements.btnConfirmSaveCache.disabled = true;
          elements.btnConfirmSaveCache.textContent = 'Salvando...';

          const res = await fetch('/api/cache/save', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ fileName }),
          });

          if (!res.ok) throw new Error(await res.text());
          const data = await res.json();

          showToast(`Snapshot de cache salvo com sucesso em: ${data.filePath}`, 'success');
          elements.saveCacheModal.classList.add('hidden');
        } catch (err) {
          showToast(`Erro ao salvar cache: ${err.message}`, 'danger');
        } finally {
          elements.btnConfirmSaveCache.disabled = false;
          elements.btnConfirmSaveCache.textContent = 'Salvar Snapshot';
        }
      });
    }

    // Open Load Cache Modal
    if (elements.btnOpenLoadCacheModal) {
      elements.btnOpenLoadCacheModal.addEventListener('click', () => {
        elements.loadCacheModal.classList.remove('hidden');
        fetchSavedCachesList();
      });
    }

    // Load Custom Cache Path
    if (elements.btnLoadCustomCache) {
      elements.btnLoadCustomCache.addEventListener('click', () => {
        const path = elements.customCachePath.value.trim();
        if (!path) {
          showToast('Informe o caminho do arquivo de cache!', 'danger');
          return;
        }
        loadCacheFile(path);
      });
    }
  }

  // Fetch list of saved caches on disk
  async function fetchSavedCachesList() {
    try {
      elements.savedCachesList.innerHTML = '<div class="loading-state">Buscando snapshots salvos na pasta ./saved_scans/...</div>';
      const res = await fetch('/api/cache/list');
      if (!res.ok) throw new Error('Falha ao listar caches');
      const list = await res.json();

      if (!list || list.length === 0) {
        elements.savedCachesList.innerHTML = '<div class="empty-state">Nenhum cache salvo encontrado na pasta ./saved_scans/</div>';
        return;
      }

      elements.savedCachesList.innerHTML = list.map(item => {
        const dateStr = new Date(item.modTime).toLocaleString('pt-BR');
        return `
          <div class="saved-cache-card">
            <div class="saved-cache-info">
              <div class="saved-cache-name">${item.fileName}</div>
              <div class="saved-cache-meta">
                <span>📅 ${dateStr}</span>
                <span>📦 ${formatBytes(item.sizeBytes)}</span>
              </div>
            </div>
            <button class="btn btn-primary btn-sm btn-load-cache-file" data-path="${item.filePath}">
              Carregar Snapshot
            </button>
          </div>
        `;
      }).join('');

      elements.savedCachesList.querySelectorAll('.btn-load-cache-file').forEach(btn => {
        btn.addEventListener('click', () => {
          const path = btn.getAttribute('data-path');
          loadCacheFile(path);
        });
      });
    } catch (err) {
      elements.savedCachesList.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
    }
  }

  // Execute loading of cache file
  async function loadCacheFile(filePath) {
    try {
      setGlobalOperationLock(
        true, 
        'Carregando Snapshot de Cache para a Memória...', 
        `Descompactando ${filePath} e reconstruindo árvore de arquivos e índices...`, 
        10, 
        'Lendo arquivo do disco...'
      );

      showToast(`Carregando cache de ${filePath}...`, 'info');
      const res = await fetch('/api/cache/load', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ filePath }),
      });

      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();

      elements.loadCacheModal.classList.add('hidden');
      updateGlobalProgress(100, 'Snapshot carregado com sucesso!');
      showToast(`Cache carregado! ${formatNumber(data.snapshot.totalFiles)} arquivos (${formatBytes(data.snapshot.totalBytes)}) restaurados em memória.`, 'success');

      // Update UI displays
      elements.statFiles.textContent = formatNumber(data.snapshot.totalFiles);
      elements.statDirs.textContent = formatNumber(data.snapshot.totalDirs);
      elements.statBytes.textContent = formatBytes(data.snapshot.totalBytes);
      elements.currentPathText.textContent = `Snapshot carregado: ${data.snapshot.fileName || filePath}`;

      // Refresh other tabs
      loadDuplicates(true);
      loadFolderDuplicates(true);
      loadTreeData('');
      loadAnalytics();
    } catch (err) {
      showToast(`Erro ao carregar cache: ${err.message}`, 'danger');
    } finally {
      setTimeout(() => setGlobalOperationLock(false), 350);
    }
  }

  // Setup Folder Comparator
  function setupFolderComparator() {
    // Subtabs switcher
    document.querySelectorAll('.folder-subtab').forEach(btn => {
      btn.addEventListener('click', () => {
        const targetSubtab = btn.getAttribute('data-subtab');
        document.querySelectorAll('.folder-subtab').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.folder-subtab-pane').forEach(p => p.classList.remove('active'));

        btn.classList.add('active');
        const pane = document.getElementById(targetSubtab);
        if (pane) pane.classList.add('active');
        state.currentFolderSubtab = targetSubtab;

        if (targetSubtab === 'folderDupSubtab') {
          loadFolderDuplicates();
        }
      });
    });

    if (elements.dupFolderSortBy) elements.dupFolderSortBy.addEventListener('change', () => loadFolderDuplicates(true));
    if (elements.dupFolderMinSize) elements.dupFolderMinSize.addEventListener('change', () => loadFolderDuplicates(true));
    if (elements.dupFolderSearch) elements.dupFolderSearch.addEventListener('input', debounce(() => loadFolderDuplicates(true), 300));
    if (elements.chkFolderTopLevelOnly) elements.chkFolderTopLevelOnly.addEventListener('change', () => loadFolderDuplicates(true));
    if (elements.btnRefreshFolderDuplicates) elements.btnRefreshFolderDuplicates.addEventListener('click', () => loadFolderDuplicates(true));

    // Swap paths
    if (elements.btnSwapComparePaths) {
      elements.btnSwapComparePaths.addEventListener('click', () => {
        const tmp = elements.comparePathA.value;
        elements.comparePathA.value = elements.comparePathB.value;
        elements.comparePathB.value = tmp;
      });
    }

    // Run comparison
    if (elements.btnRunFolderCompare) {
      elements.btnRunFolderCompare.addEventListener('click', runFolderCompare);
    }
  }

  // Load Folder Duplicates
  async function loadFolderDuplicates(page = 1) {
    if (!elements.folderDuplicatesContainer || state.isLoadingFolderDups) return;

    state.folderDupPage = Math.max(1, page);
    const limit = state.folderDupLimit || 50;
    const offset = (state.folderDupPage - 1) * limit;

    const sortBy = elements.dupFolderSortBy ? elements.dupFolderSortBy.value : 'subdirs_desc';
    const minSize = elements.dupFolderMinSize ? elements.dupFolderMinSize.value : 0;
    const search = elements.dupFolderSearch ? elements.dupFolderSearch.value.trim() : '';
    const topLevelOnly = elements.chkFolderTopLevelOnly ? elements.chkFolderTopLevelOnly.checked : true;

    try {
      state.isLoadingFolderDups = true;
      if (elements.folderDuplicatesContainer) {
        elements.folderDuplicatesContainer.innerHTML = '<div class="loading-state">Identificando e classificando pastas clones por hierarquia...</div>';
      }

      const url = `/api/folders/duplicates?sortBy=${sortBy}&minSize=${minSize}&search=${encodeURIComponent(search)}&topLevelOnly=${topLevelOnly}&limit=${limit}&offset=${offset}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Falha ao obter pastas duplicadas');
      const data = await res.json();
      state.folderDuplicatesData = data;
      state.folderDupGroups = data.groups || [];
      state.folderDupTotalGroups = data.totalGroups || 0;

      renderFolderDuplicates(data);
    } catch (err) {
      if (elements.folderDuplicatesContainer) {
        elements.folderDuplicatesContainer.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
      }
    } finally {
      state.isLoadingFolderDups = false;
    }
  }

  // Render Duplicate Folders
  function renderFolderDuplicates(data) {
    if (!elements.folderDuplicatesContainer) return;

    if (!data.groups || data.groups.length === 0) {
      elements.folderDuplicatesContainer.innerHTML = '<div class="empty-state">Nenhuma pasta duplicada encontrada com os filtros atuais. Realize uma varredura para identificar pastas com conteúdo idêntico.</div>';
      if (elements.dupFolderTotalGroups) elements.dupFolderTotalGroups.textContent = '0';
      if (elements.dupFolderTotalCount) elements.dupFolderTotalCount.textContent = '0';
      if (elements.dupFolderTotalWasted) elements.dupFolderTotalWasted.textContent = '0 B';
      if (elements.folderDupPaginationBar) elements.folderDupPaginationBar.innerHTML = '';
      return;
    }

    const isFilteredTop = elements.chkFolderTopLevelOnly && elements.chkFolderTopLevelOnly.checked;
    const totalTxt = isFilteredTop && data.topLevelGroups 
      ? `${formatNumber(data.totalGroups)} (${formatNumber(data.topLevelGroups)} Pastas Raiz)`
      : formatNumber(data.totalGroups || 0);

    if (elements.dupFolderTotalGroups) elements.dupFolderTotalGroups.textContent = totalTxt;
    if (elements.dupFolderTotalCount) elements.dupFolderTotalCount.textContent = formatNumber(data.totalFolders || 0);
    if (elements.dupFolderTotalWasted) elements.dupFolderTotalWasted.textContent = formatBytes(data.wastedBytes || 0);
    elements.folderDuplicatesContainer.innerHTML = '';

    const fragment = document.createDocumentFragment();

    data.groups.forEach(grp => {
      const hashShort = (grp.folderHash && grp.folderHash.length > 24) ? grp.folderHash.substring(0, 24) + '...' : (grp.folderHash || 'hash');
      const isRoot = grp.isTopLevel !== false;
      const levelBadge = isRoot 
        ? `<span class="dup-root-badge">🌟 PASTA RAIZ CLONE</span>` 
        : `<span class="dup-subfolder-badge">📁 Subpasta Contida</span>`;
      const subdirsBadge = grp.subDirCount > 0 
        ? `<span class="dup-subdirs-pill" title="${formatNumber(grp.subDirCount)} subpastas nesta árvore">🌳 ${formatNumber(grp.subDirCount)} subpastas</span>` 
        : '';

      const card = document.createElement('div');
      card.className = `dup-group-card folder-dup-group-card ${isRoot ? 'is-root-clone' : ''}`;
      card.setAttribute('data-group-id', grp.id);

      card.innerHTML = `
        <div class="dup-group-header">
          <div class="dup-group-left">
            ${levelBadge}
            <span class="dup-hash-pill" title="${grp.folderHash}">📁 ${hashShort}</span>
            <span class="dup-size-pill">${formatBytes(grp.folderSize)} cada</span>
            <span class="dup-size-pill">📄 ${formatNumber(grp.fileCount)} arquivos</span>
            ${subdirsBadge}
            <span class="dup-wasted-badge">Desperdício: ${formatBytes(grp.wastedBytes)} (${grp.folderCount} cópias)</span>
          </div>
          <div class="dup-group-actions">
            ${grp.folders && grp.folders.length >= 2 ? `
              <button class="btn btn-secondary btn-sm btn-quick-compare-folders" 
                      data-path-a="${grp.folders[0].path}" 
                      data-path-b="${grp.folders[1].path}">
                ⚖️ Comparar Pasta 1 e 2
              </button>
            ` : ''}
          </div>
        </div>

        <div class="dup-file-list">
          ${(grp.folders || []).map((folder, idx) => {
            const folderSubdirs = folder.subDirCount > 0 ? ` &bull; 🌳 ${formatNumber(folder.subDirCount)} subpastas` : '';
            return `
              <div class="dup-file-row">
                <div class="dup-file-left">
                  <span class="folder-badge-num">#${idx + 1}</span>
                  <span class="dup-file-path truncate" title="${folder.path}">${folder.path}</span>
                </div>
                <div class="dup-file-date">
                  <span>${formatNumber(folder.fileCount)} arq${folderSubdirs}</span> &bull; 
                  <strong>${formatBytes(folder.totalSize)}</strong>
                </div>
              </div>
            `;
          }).join('')}
        </div>
      `;

      card.querySelectorAll('.btn-quick-compare-folders').forEach(btn => {
        btn.addEventListener('click', () => {
          const pA = btn.getAttribute('data-path-a');
          const pB = btn.getAttribute('data-path-b');
          if (elements.comparePathA) elements.comparePathA.value = pA;
          if (elements.comparePathB) elements.comparePathB.value = pB;

          const compTabBtn = document.querySelector('.folder-subtab[data-subtab="folderCompareSubtab"]');
          if (compTabBtn) compTabBtn.click();
          runFolderCompare();
        });
      });

      fragment.appendChild(card);
    });

    elements.folderDuplicatesContainer.appendChild(fragment);

    // Universal pagination bar
    renderPaginationBar(
      elements.folderDupPaginationBar,
      data.totalGroups || 0,
      state.folderDupPage,
      state.folderDupLimit,
      (newPage) => loadFolderDuplicates(newPage),
      (newLimit) => {
        state.folderDupLimit = newLimit;
        loadFolderDuplicates(1);
      }
    );
  }

  // Run Folder Direct Side-by-Side Comparison
  async function runFolderCompare() {
    const pathA = elements.comparePathA.value.trim();
    const pathB = elements.comparePathB.value.trim();

    if (!pathA || !pathB) {
      showToast('Por favor, informe o caminho da Pasta A e da Pasta B!', 'danger');
      return;
    }

    if (pathA.toLowerCase() === pathB.toLowerCase()) {
      showToast('A Pasta A e a Pasta B são o mesmo diretório!', 'danger');
      return;
    }

    try {
      elements.btnRunFolderCompare.disabled = true;
      elements.btnRunFolderCompare.innerHTML = '<span class="loading-spinner"></span> Comparando...';
      elements.folderCompareResults.classList.remove('hidden');
      elements.folderCompareResults.innerHTML = '<div class="loading-state">Calculando árvores de arquivos, hashes e analisando diferenças...</div>';

      const url = `/api/folders/compare?pathA=${encodeURIComponent(pathA)}&pathB=${encodeURIComponent(pathB)}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      state.folderComparisonData = data;
      state.folderDiffFilter = 'ALL';
      renderFolderComparison(data);
    } catch (err) {
      elements.folderCompareResults.innerHTML = `<div class="empty-state">Erro na comparação: ${err.message}</div>`;
    } finally {
      elements.btnRunFolderCompare.disabled = false;
      elements.btnRunFolderCompare.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"></circle><line x1="21" y1="21" x2="16.65" y2="16.65"></line></svg>
        Comparar Conteúdo das Pastas (Hash & Arquivos)
      `;
    }
  }

  // Render Folder Comparison Results
  function renderFolderComparison(data) {
    if (!data) return;

    const isMatch = data.is100PercentMatch;
    const matchPct = (data.matchPercentage || 0).toFixed(1);

    elements.folderCompareResults.innerHTML = `
      <div class="compare-match-banner ${isMatch ? 'match-identical' : 'match-different'}">
        <div class="match-badge-big">
          ${isMatch ? '✅ PASTAS 100% IDÊNTICAS (Mesmo Conteúdo e Hash)' : `⚠️ PASTAS COM CONTEÚDO DIFERENTE (${matchPct}% correspondência)`}
        </div>
        <p class="match-desc">
          ${isMatch ? 
            'Todos os arquivos em ambas as pastas possuem exatamente o mesmo caminho relativo, mesmo tamanho e mesmo hash de conteúdo.' : 
            `Foram encontradas divergências: ${data.modifiedCount} arquivos modificados, ${data.onlyInACount} exclusivos na Pasta A e ${data.onlyInBCount} exclusivos na Pasta B.`
          }
        </p>
      </div>

      <!-- Side by Side Overview -->
      <div class="compare-overview-grid">
        <div class="compare-folder-card">
          <div class="compare-card-title">Pasta A</div>
          <div class="compare-folder-path truncate" title="${data.pathA}">${data.pathA}</div>
          <div class="compare-metric-row">
            <span>Tamanho Total:</span>
            <strong>${formatBytes(data.totalSizeA)}</strong>
          </div>
          <div class="compare-metric-row">
            <span>Total de Arquivos:</span>
            <strong>${formatNumber(data.totalFilesA)} arquivos</strong>
          </div>
          <div class="compare-metric-row">
            <span>Folder Content Hash:</span>
            <code class="hash-code truncate" title="${data.folderHashA}">${data.folderHashA}</code>
          </div>
        </div>

        <div class="compare-folder-card">
          <div class="compare-card-title">Pasta B</div>
          <div class="compare-folder-path truncate" title="${data.pathB}">${data.pathB}</div>
          <div class="compare-metric-row">
            <span>Tamanho Total:</span>
            <strong>${formatBytes(data.totalSizeB)}</strong>
          </div>
          <div class="compare-metric-row">
            <span>Total de Arquivos:</span>
            <strong>${formatNumber(data.totalFilesB)} arquivos</strong>
          </div>
          <div class="compare-metric-row">
            <span>Folder Content Hash:</span>
            <code class="hash-code truncate" title="${data.folderHashB}">${data.folderHashB}</code>
          </div>
        </div>
      </div>

      <!-- Diff Filter Toolbar -->
      <div class="diff-filter-toolbar">
        <div class="diff-filter-tabs">
          <button class="btn btn-secondary btn-sm diff-filter-btn ${state.folderDiffFilter === 'ALL' ? 'active' : ''}" data-filter="ALL">
            Todos (${data.diffEntries.length})
          </button>
          <button class="btn btn-secondary btn-sm diff-filter-btn ${state.folderDiffFilter === 'IDENTICAL' ? 'active' : ''}" data-filter="IDENTICAL">
            ✅ Idênticos (${data.identicalCount})
          </button>
          <button class="btn btn-secondary btn-sm diff-filter-btn ${state.folderDiffFilter === 'MODIFIED' ? 'active' : ''}" data-filter="MODIFIED">
            🔄 Modificados (${data.modifiedCount})
          </button>
          <button class="btn btn-secondary btn-sm diff-filter-btn ${state.folderDiffFilter === 'ONLY_IN_A' ? 'active' : ''}" data-filter="ONLY_IN_A">
            🅰️ Apenas em A (${data.onlyInACount})
          </button>
          <button class="btn btn-secondary btn-sm diff-filter-btn ${state.folderDiffFilter === 'ONLY_IN_B' ? 'active' : ''}" data-filter="ONLY_IN_B">
            🅱️ Apenas em B (${data.onlyInBCount})
          </button>
        </div>
        <input type="text" id="diffSearchInput" class="form-input form-input-sm" placeholder="Filtrar por nome do arquivo...">
      </div>

      <!-- Diff Files Table -->
      <div class="diff-table-wrapper">
        <table class="diff-table">
          <thead>
            <tr>
              <th style="width: 130px;">Status</th>
              <th>Caminho Relativo</th>
              <th style="width: 140px;">Tamanho A</th>
              <th style="width: 140px;">Tamanho B</th>
              <th style="width: 220px;">Hash A / B</th>
            </tr>
          </thead>
          <tbody id="folderDiffTableBody">
            <!-- Populated below -->
          </tbody>
        </table>
      </div>
    `;

    renderDiffEntriesTable();

    // Attach diff filter button listeners
    elements.folderCompareResults.querySelectorAll('.diff-filter-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        state.folderDiffFilter = btn.getAttribute('data-filter');
        elements.folderCompareResults.querySelectorAll('.diff-filter-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        renderDiffEntriesTable();
      });
    });

    // Attach search input listener
    const diffSearch = document.getElementById('diffSearchInput');
    if (diffSearch) {
      diffSearch.addEventListener('input', debounce(renderDiffEntriesTable, 250));
    }
  }

  function renderDiffEntriesTable() {
    const tbody = document.getElementById('folderDiffTableBody');
    if (!tbody || !state.folderComparisonData) return;

    let entries = state.folderComparisonData.diffEntries || [];
    const filter = state.folderDiffFilter || 'ALL';

    if (filter !== 'ALL') {
      entries = entries.filter(e => e.status === filter);
    }

    const searchEl = document.getElementById('diffSearchInput');
    const search = searchEl ? searchEl.value.toLowerCase().trim() : '';
    if (search) {
      entries = entries.filter(e => e.relativePath.toLowerCase().includes(search));
    }

    if (entries.length === 0) {
      tbody.innerHTML = '<tr><td colspan="5" class="empty-state">Nenhum arquivo encontrado com os filtros selecionados.</td></tr>';
      return;
    }

    tbody.innerHTML = entries.map(e => {
      let statusBadge = '';
      if (e.status === 'IDENTICAL') {
        statusBadge = '<span class="status-pill status-OK">Idêntico</span>';
      } else if (e.status === 'MODIFIED') {
        statusBadge = '<span class="status-pill status-ERROR">Modificado</span>';
      } else if (e.status === 'ONLY_IN_A') {
        statusBadge = '<span class="status-pill status-LOCKED">Apenas em A</span>';
      } else if (e.status === 'ONLY_IN_B') {
        statusBadge = '<span class="status-pill status-READING">Apenas em B</span>';
      }

      const sizeA = e.sizeA > 0 ? formatBytes(e.sizeA) : (e.status === 'ONLY_IN_B' ? '-' : '0 B');
      const sizeB = e.sizeB > 0 ? formatBytes(e.sizeB) : (e.status === 'ONLY_IN_A' ? '-' : '0 B');

      const hashA = e.hashA ? (e.hashA.length > 14 ? e.hashA.substring(0, 14) + '...' : e.hashA) : '-';
      const hashB = e.hashB ? (e.hashB.length > 14 ? e.hashB.substring(0, 14) + '...' : e.hashB) : '-';

      return `
        <tr>
          <td>${statusBadge}</td>
          <td class="truncate" title="${e.relativePath}">${e.relativePath}</td>
          <td><strong>${sizeA}</strong></td>
          <td><strong>${sizeB}</strong></td>
          <td>
            <small style="color:var(--accent-cyan); display:block;">A: ${hashA}</small>
            <small style="color:var(--accent-purple); display:block;">B: ${hashB}</small>
          </td>
        </tr>
      `;
    }).join('');
  }

  // Load Idle / Stale Files
  async function loadIdleFiles(page = 1) {
    if (!elements.idleTableBody) return;

    state.idlePage = Math.max(1, page);
    const limit = state.idleLimit || 50;
    const offset = (state.idlePage - 1) * limit;

    const minAge = elements.idleMinAge ? elements.idleMinAge.value : 365;
    const minSize = elements.idleMinSize ? elements.idleMinSize.value : 104857600;
    const search = elements.idleSearch ? elements.idleSearch.value.trim() : '';
    const sortBy = elements.idleSortBy ? elements.idleSortBy.value : 'size_desc';

    try {
      elements.idleTableBody.innerHTML = '<tr><td colspan="6" class="loading-state">Analisando datas de modificação e calculando idades dos arquivos...</td></tr>';
      const url = `/api/stats/idle-files?minAgeDays=${minAge}&minSize=${minSize}&search=${encodeURIComponent(search)}&sortBy=${sortBy}&offset=${offset}&limit=${limit}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Falha ao consultar arquivos ociosos');
      const data = await res.json();
      state.idleData = data;
      renderIdleFiles(data);
    } catch (err) {
      elements.idleTableBody.innerHTML = `<tr><td colspan="6" class="empty-state">Erro: ${err.message}</td></tr>`;
    }
  }

  // Render Idle Files View
  function renderIdleFiles(data) {
    if (!data) return;

    if (elements.idleTotalCount) elements.idleTotalCount.textContent = formatNumber(data.totalIdleFiles || 0);
    if (elements.idleTotalBytes) elements.idleTotalBytes.textContent = formatBytes(data.totalIdleBytes || 0);

    // Render Age Buckets Grid
    const buckets = data.buckets || data.ageBuckets || [];
    if (elements.idleBucketsGrid && buckets.length > 0) {
      elements.idleBucketsGrid.innerHTML = buckets.map(b => `
        <div class="idle-bucket-card">
          <div class="idle-bucket-label">${b.label}</div>
          <div class="idle-bucket-size">${formatBytes(b.totalBytes)}</div>
          <div class="idle-bucket-count">${formatNumber(b.fileCount)} arquivos</div>
        </div>
      `).join('');
    }

    const files = data.topFiles || data.files || [];
    if (files.length === 0) {
      elements.idleTableBody.innerHTML = '<tr><td colspan="6" class="empty-state">Nenhum arquivo ocioso encontrado com os filtros atuais.</td></tr>';
      if (elements.idlePaginationBar) elements.idlePaginationBar.innerHTML = '';
      updateIdleSelectionSummary();
      return;
    }

    elements.idleTableBody.innerHTML = files.map(file => {
      const isMarked = state.selectedIdleFiles.has(file.path);
      const inactiveDays = file.daysInactive !== undefined ? file.daysInactive : (file.inactiveDays || 0);
      const years = (inactiveDays / 365.25).toFixed(1);
      const ageLabel = inactiveDays >= 365 ? `${years} anos (${formatNumber(inactiveDays)} dias)` : `${formatNumber(inactiveDays)} dias`;

      return `
        <tr class="${isMarked ? 'marked-for-delete' : ''}">
          <td>
            <input type="checkbox" class="idle-file-checkbox" 
                   data-path="${file.path}" 
                   data-size="${file.size}" 
                   ${isMarked ? 'checked' : ''}>
          </td>
          <td class="truncate" title="${file.path}">
            <div class="tree-node-cell">
              <span class="tree-icon file-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"></path><polyline points="13 2 13 9 20 9"></polyline></svg>
              </span>
              <span class="tree-node-name">${file.path}</span>
            </div>
          </td>
          <td><strong>${formatBytes(file.size)}</strong></td>
          <td><span class="idle-age-badge">${ageLabel}</span></td>
          <td>${formatDate(file.createTime)}</td>
          <td>${formatDate(file.modTime)}</td>
        </tr>
      `;
    }).join('');

    // Checkbox event listeners
    elements.idleTableBody.querySelectorAll('.idle-file-checkbox').forEach(cb => {
      cb.addEventListener('change', () => {
        const path = cb.getAttribute('data-path');
        const size = parseInt(cb.getAttribute('data-size'), 10);
        if (cb.checked) {
          state.selectedIdleFiles.set(path, size);
        } else {
          state.selectedIdleFiles.delete(path);
        }
        cb.closest('tr').classList.toggle('marked-for-delete', cb.checked);
        updateIdleSelectionSummary();
      });
    });

    // Universal pagination bar
    renderPaginationBar(
      elements.idlePaginationBar,
      data.totalIdleFiles || 0,
      state.idlePage,
      state.idleLimit,
      (newPage) => loadIdleFiles(newPage),
      (newLimit) => {
        state.idleLimit = newLimit;
        loadIdleFiles(1);
      }
    );

    updateIdleSelectionSummary();
  }


  function selectAllIdleFiles() {
    if (!state.idleData || !state.idleData.topFiles) return;
    state.idleData.topFiles.forEach(f => {
      state.selectedIdleFiles.set(f.path, f.size);
    });
    renderIdleFiles(state.idleData);
    if (elements.idleSelectAllCheckbox) elements.idleSelectAllCheckbox.checked = true;
  }

  function clearIdleSelection() {
    state.selectedIdleFiles.clear();
    renderIdleFiles(state.idleData);
    if (elements.idleSelectAllCheckbox) elements.idleSelectAllCheckbox.checked = false;
  }

  function updateIdleSelectionSummary() {
    const count = state.selectedIdleFiles.size;
    let totalBytes = 0;
    state.selectedIdleFiles.forEach(sz => totalBytes += sz);

    if (elements.idleSelectedCount) {
      elements.idleSelectedCount.textContent = `${count} (${formatBytes(totalBytes)})`;
    }
    if (elements.btnRecycleIdleSelected) {
      elements.btnRecycleIdleSelected.disabled = count === 0;
      elements.btnRecycleIdleSelected.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path></svg>
        Mover ${count} Marcados para a Lixeira (${formatBytes(totalBytes)})
      `;
    }
  }

  // Recycle Selected Idle Files
  async function recycleIdleSelectedFiles() {
    const files = Array.from(state.selectedIdleFiles.keys());
    if (files.length === 0) return;

    let totalBytes = 0;
    state.selectedIdleFiles.forEach(sz => totalBytes += sz);

    const confirmed = confirm(
      `Tem certeza que deseja enviar ${files.length} arquivo(s) ociosos (${formatBytes(totalBytes)}) para a LIXEIRA DO WINDOWS?\n\n` +
      `Os arquivos poderão ser recuperados na Lixeira nativa do Windows caso necessário.`
    );

    if (!confirmed) return;

    try {
      const res = await fetch('/api/files/recycle', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ paths: files }),
      });

      if (!res.ok) throw new Error(await res.text());
      const result = await res.json();

      showToast(`Sucesso! ${result.successCount} arquivo(s) enviados para a Lixeira do Windows. (${formatBytes(result.freedBytes)} liberados)`, 'success');
      state.selectedIdleFiles.clear();
      loadIdleFiles();
    } catch (err) {
      showToast('Erro ao reciclar arquivos: ' + err.message, 'danger');
    }
  }

  // =========================================================================
  // AI ASSISTANT & MCP AGENT IMPLEMENTATION
  // =========================================================================

  function setupAIAssistant() {
    // Provider Switcher Buttons
    document.querySelectorAll('.btn-ai-provider').forEach(btn => {
      btn.addEventListener('click', () => {
        const provider = btn.getAttribute('data-provider');
        document.querySelectorAll('.btn-ai-provider').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        state.ai.provider = provider;
        updateAIModelDropdown();
      });
    });

    // Model Selector Change
    if (elements.aiModelSelect) {
      elements.aiModelSelect.addEventListener('change', () => {
        state.ai.selectedModel = elements.aiModelSelect.value;
        checkIfModelNeedsPull();
      });
    }

    // Pull Model Button
    if (elements.btnPullSelectedModel) {
      elements.btnPullSelectedModel.addEventListener('click', () => {
        if (state.ai.selectedModel) {
          pullOllamaModel(state.ai.selectedModel);
        }
      });
    }

    // Settings Modal
    if (elements.btnOpenAIConfigModal && elements.aiConfigModal) {
      elements.btnOpenAIConfigModal.addEventListener('click', async () => {
        elements.aiConfigModal.classList.remove('hidden');
        try {
          const res = await fetch('/api/config');
          if (res.ok) {
            const cfg = await res.json();
            if (elements.aiOllamaEndpointInput) elements.aiOllamaEndpointInput.value = cfg.aiOllamaEndpoint || 'http://127.0.0.1:11434';
            if (elements.aiOpenRouterKeyInput) elements.aiOpenRouterKeyInput.value = cfg.aiOpenRouterKey || '';
            if (elements.aiDryRunDefaultCheckbox) elements.aiDryRunDefaultCheckbox.checked = cfg.aiDryRunDefault !== false;
          }
        } catch (e) {
          console.error('Falha ao carregar config de IA', e);
        }
      });
    }

    // Save AI Settings
    if (elements.btnSaveAIConfig) {
      elements.btnSaveAIConfig.addEventListener('click', async () => {
        try {
          const resConfig = await fetch('/api/config');
          const currentConfig = resConfig.ok ? await resConfig.json() : {};

          currentConfig.aiOllamaEndpoint = elements.aiOllamaEndpointInput.value.trim() || 'http://127.0.0.1:11434';
          currentConfig.aiOpenRouterKey = elements.aiOpenRouterKeyInput.value.trim();
          currentConfig.aiDryRunDefault = elements.aiDryRunDefaultCheckbox.checked;

          const saveRes = await fetch('/api/config', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(currentConfig),
          });

          if (saveRes.ok) {
            showToast('Configurações de IA salvas com sucesso!', 'success');
            elements.aiConfigModal.classList.add('hidden');
            loadAICatalog();
          } else {
            showToast('Falha ao salvar configurações de IA.', 'danger');
          }
        } catch (err) {
          showToast('Erro: ' + err.message, 'danger');
        }
      });
    }

    // Clear Chat
    if (elements.btnClearAIChat) {
      elements.btnClearAIChat.addEventListener('click', () => {
        state.ai.chatHistory = [];
        if (elements.aiMessagesContainer) {
          elements.aiMessagesContainer.innerHTML = `
            <div class="ai-welcome-message">
              <div class="ai-avatar">🤖</div>
              <div class="ai-bubble">
                <h4>Conversa reiniciada.</h4>
                <p>Peça para eu analisar arquivos, checar duplicatas ou propor ações seguras.</p>
                <div class="ai-quick-prompts">
                  <button class="ai-prompt-chip" data-prompt="Ache todas as duplicatas maiores que 50MB e gere uma proposta de limpeza">
                    🔍 Achar duplicatas &gt; 50MB
                  </button>
                  <button class="ai-prompt-chip" data-prompt="Classifique os maiores arquivos deste disco por tipo e resuma o espaço">
                    📊 Classificar maiores arquivos
                  </button>
                  <button class="ai-prompt-chip" data-prompt="Quais são os arquivos ociosos há mais de 1 ano que estão ocupando mais espaço?">
                    ⏳ Arquivos ociosos &gt; 1 ano
                  </button>
                </div>
              </div>
            </div>
          `;
          attachQuickPromptListeners();
        }
      });
    }

    // Send Message Button & Textarea
    if (elements.btnSendAIMessage) {
      elements.btnSendAIMessage.addEventListener('click', () => sendAIMessage());
    }

    if (elements.aiPromptInput) {
      elements.aiPromptInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter' && !e.shiftKey) {
          e.preventDefault();
          sendAIMessage();
        }
      });
    }

    attachQuickPromptListeners();
  }

  function attachQuickPromptListeners() {
    document.querySelectorAll('.ai-prompt-chip').forEach(chip => {
      chip.addEventListener('click', () => {
        const prompt = chip.getAttribute('data-prompt');
        if (elements.aiPromptInput) {
          elements.aiPromptInput.value = prompt;
          sendAIMessage();
        }
      });
    });
  }

  // Load AI Catalog from Server
  async function loadAICatalog() {
    try {
      const res = await fetch('/api/ai/models');
      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();
      state.ai.modelsCatalog = data;

      // Update status dot
      if (elements.ollamaStatusDot) {
        elements.ollamaStatusDot.className = `provider-dot ${data.ollamaOnline ? 'online' : ''}`;
        elements.ollamaStatusDot.title = data.ollamaOnline ? `Ollama Online (${data.ollamaVersion || 'v0.x'})` : 'Ollama Offline';
      }

      updateAIModelDropdown();
      renderCatalogSidebar(data);
    } catch (err) {
      console.warn('Erro ao carregar catálogo de IA:', err);
    }
  }

  function updateAIModelDropdown() {
    if (!elements.aiModelSelect || !state.ai.modelsCatalog) return;

    const cat = state.ai.modelsCatalog;
    let list = [];

    if (state.ai.provider === 'ollama') {
      list = cat.localModels || [];
    } else if (state.ai.provider === 'openrouter') {
      list = cat.openRouterModels || [];
    } else if (state.ai.provider === 'direct') {
      list = cat.directModels && cat.directModels.length > 0 ? cat.directModels : [
        { id: 'direct-rules', name: 'Regras e IA Local Direta (In-Process)', ramRequired: '~50 MB RAM', isInstalled: true }
      ];
    }

    elements.aiModelSelect.innerHTML = list.map(m => {
      const installedBadge = m.isInstalled ? ' [Instalado]' : (m.isFree ? ' [Grátis]' : '');
      const ramInfo = m.ramRequired ? ` (${m.ramRequired})` : '';
      return `<option value="${m.id}">${m.name}${ramInfo}${installedBadge}</option>`;
    }).join('');

    // Select first or previously selected
    if (list.length > 0) {
      const found = list.some(m => m.id === state.ai.selectedModel);
      if (!found) {
        state.ai.selectedModel = list[0].id;
      }
      elements.aiModelSelect.value = state.ai.selectedModel;
    }

    checkIfModelNeedsPull();
  }

  function checkIfModelNeedsPull() {
    if (!elements.btnPullSelectedModel || !state.ai.modelsCatalog) return;

    if (state.ai.provider !== 'ollama') {
      elements.btnPullSelectedModel.classList.add('hidden');
      return;
    }

    const currentModelId = elements.aiModelSelect ? elements.aiModelSelect.value : state.ai.selectedModel;
    const cat = state.ai.modelsCatalog.localModels || [];
    const item = cat.find(m => m.id === currentModelId);

    if (item && !item.isInstalled && state.ai.modelsCatalog.ollamaOnline) {
      elements.btnPullSelectedModel.classList.remove('hidden');
      elements.btnPullSelectedModel.textContent = `⬇️ Baixar ${item.parameters || item.id} (${item.downloadSize})`;
    } else {
      elements.btnPullSelectedModel.classList.add('hidden');
    }
  }

  function renderCatalogSidebar(catalog) {
    if (!elements.aiModelsList) return;

    const models = catalog.localModels || [];
    if (models.length === 0) {
      elements.aiModelsList.innerHTML = '<div class="empty-state">Nenhum modelo disponível.</div>';
      return;
    }

    elements.aiModelsList.innerHTML = models.map(m => {
      const isSelected = m.id === state.ai.selectedModel;
      const statusHtml = m.isInstalled 
        ? '<span class="model-status-tag">✅ Pronto para Uso</span>' 
        : '<span class="model-status-tag not-installed">☁️ Download Necessário</span>';

      const visionBadge = m.supportsVision 
        ? '<span class="model-param-badge" style="background: rgba(16, 185, 129, 0.25); color: #34d399;" title="Capacidade Multimodal: Leitura de imagens, fotos e OCR em PDFs">👁️ Visão & OCR</span>' 
        : '';

      const primaryTag = m.isPrimary 
        ? '<div style="font-size:0.7rem; font-weight:800; color:#fbbf24; margin-bottom:0.2rem;">⭐ MODELO PRIMÁRIO RECOMENDADO</div>' 
        : '';

      return `
        <div class="model-catalog-item ${isSelected ? 'active-model' : ''} ${m.isPrimary ? 'primary-model-card' : ''}" data-model-id="${m.id}">
          ${primaryTag}
          <div class="model-item-header">
            <span class="model-item-name">${m.name}</span>
            <div style="display:flex; gap:0.3rem;">
              ${visionBadge}
              <span class="model-param-badge">${m.parameters || 'SLM'}</span>
            </div>
          </div>
          <div class="model-item-specs">
            <span>💾 ${m.downloadSize}</span>
            <span>⚡ ${m.ramRequired}</span>
          </div>
          <p class="model-item-desc">${m.recommendedFor || ''}</p>
          <div class="model-item-footer">
            ${statusHtml}
            <button class="btn btn-secondary btn-sm btn-select-catalog-model" data-model-id="${m.id}">
              ${m.isInstalled ? 'Selecionar' : '⬇️ Baixar'}
            </button>
          </div>
        </div>
      `;
    }).join('');

    // Attach click events on catalog items
    elements.aiModelsList.querySelectorAll('.btn-select-catalog-model').forEach(btn => {
      btn.addEventListener('click', (e) => {
        e.stopPropagation();
        const modelId = btn.getAttribute('data-model-id');
        const item = models.find(m => m.id === modelId);
        
        state.ai.provider = 'ollama';
        document.querySelectorAll('.btn-ai-provider').forEach(b => {
          b.classList.toggle('active', b.getAttribute('data-provider') === 'ollama');
        });
        
        updateAIModelDropdown();
        state.ai.selectedModel = modelId;
        if (elements.aiModelSelect) elements.aiModelSelect.value = modelId;

        if (item && !item.isInstalled) {
          pullOllamaModel(modelId);
        } else {
          showToast(`Modelo '${modelId}' selecionado com sucesso!`, 'info');
          checkIfModelNeedsPull();
          renderCatalogSidebar(state.ai.modelsCatalog);
        }
      });
    });
  }

  // Pull Ollama Model with Progress Stream
  async function pullOllamaModel(modelName) {
    if (state.ai.isPulling) return;
    state.ai.isPulling = true;

    if (elements.aiPullProgressContainer) {
      elements.aiPullProgressContainer.classList.remove('hidden');
      elements.pullModelTitle.textContent = `Baixando modelo '${modelName}' via Ollama...`;
      elements.pullModelPercent.textContent = '0%';
      elements.pullProgressBar.style.width = '0%';
      elements.pullModelStatus.textContent = 'Iniciando download...';
    }

    try {
      const response = await fetch('/api/ai/models/pull', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ model: modelName }),
      });

      if (!response.ok) throw new Error(await response.text());

      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const jsonStr = line.replace('data: ', '').trim();
            if (!jsonStr) continue;
            try {
              const data = JSON.parse(jsonStr);
              if (data.status) {
                elements.pullModelStatus.textContent = data.status;
              }
              if (data.percent !== undefined) {
                const p = Math.round(data.percent);
                elements.pullModelPercent.textContent = `${p}%`;
                elements.pullProgressBar.style.width = `${p}%`;
              }
            } catch (e) {}
          }
        }
      }

      showToast(`Modelo '${modelName}' baixado e instalado com sucesso!`, 'success');
      await loadAICatalog();
    } catch (err) {
      showToast('Falha no download do modelo: ' + err.message, 'danger');
    } finally {
      state.ai.isPulling = false;
      setTimeout(() => {
        if (elements.aiPullProgressContainer) elements.aiPullProgressContainer.classList.add('hidden');
      }, 3000);
    }
  }

  // Send AI Chat Message with SSE Streaming
  async function sendAIMessage() {
    if (!elements.aiPromptInput) return;
    const prompt = elements.aiPromptInput.value.trim();
    if (!prompt || state.ai.isGenerating) return;

    state.ai.isGenerating = true;
    elements.aiPromptInput.value = '';
    elements.btnSendAIMessage.disabled = true;

    // Append User Message to UI
    appendChatMessage('user', prompt);

    // Create Assistant Bubble Container
    const msgId = 'msg_' + Date.now();
    const assistantMsg = document.createElement('div');
    assistantMsg.className = 'chat-msg assistant';
    assistantMsg.id = msgId;
    assistantMsg.innerHTML = `
      <div class="ai-avatar">🤖</div>
      <div class="msg-bubble">
        <div class="tool-status-container" id="${msgId}_tools"></div>
        <div class="thought-container" id="${msgId}_thought">
          <span class="tool-spinner"></span> <em>Pensando e consultando ferramentas...</em>
        </div>
        <div class="content-container" id="${msgId}_content"></div>
        <div class="proposal-container" id="${msgId}_proposals"></div>
      </div>
    `;
    elements.aiMessagesContainer.appendChild(assistantMsg);
    elements.aiMessagesContainer.scrollTop = elements.aiMessagesContainer.scrollHeight;

    const toolsEl = document.getElementById(`${msgId}_tools`);
    const thoughtEl = document.getElementById(`${msgId}_thought`);
    const contentEl = document.getElementById(`${msgId}_content`);
    const proposalsEl = document.getElementById(`${msgId}_proposals`);

    let accumulatedContent = '';

    try {
      const payload = {
        provider: state.ai.provider,
        model: state.ai.selectedModel,
        prompt: prompt,
        history: state.ai.chatHistory.slice(-8), // send last turns
        selectedFolder: state.treePath,
      };

      const response = await fetch('/api/ai/chat', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      if (!response.ok) throw new Error(await response.text());

      const reader = response.body.getReader();
      const decoder = new TextDecoder('utf-8');
      let buffer = '';

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        buffer += decoder.decode(value, { stream: true });
        const lines = buffer.split('\n');
        buffer = lines.pop() || '';

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            const jsonStr = line.replace('data: ', '').trim();
            if (!jsonStr) continue;

            try {
              const ev = JSON.parse(jsonStr);
              if (ev.type === 'thought') {
                if (thoughtEl) thoughtEl.innerHTML = `<span class="tool-spinner"></span> <em>${ev.content}</em>`;
              } else if (ev.type === 'tool_start') {
                if (thoughtEl) thoughtEl.innerHTML = '';
                const tBadge = document.createElement('div');
                tBadge.className = 'tool-status-badge';
                tBadge.id = `tool_${ev.ToolName || 'exec'}`;
                tBadge.innerHTML = `<span class="tool-spinner"></span> Executando ferramenta <strong>${ev.toolName || ''}</strong>...`;
                toolsEl.appendChild(tBadge);
              } else if (ev.type === 'tool_end') {
                const tBadge = document.getElementById(`tool_${ev.ToolName || 'exec'}`);
                if (tBadge) {
                  tBadge.innerHTML = `✅ Ferramenta <strong>${ev.toolName || ''}</strong> executada com sucesso.`;
                }
              } else if (ev.type === 'token') {
                if (thoughtEl) thoughtEl.innerHTML = '';
                accumulatedContent += ev.content;
                contentEl.innerHTML = formatMarkdownText(accumulatedContent);
              } else if (ev.type === 'proposal' && ev.proposal) {
                renderActionProposalCard(ev.proposal, proposalsEl);
              } else if (ev.type === 'error') {
                contentEl.innerHTML += `<div class="alert alert-danger" style="margin-top:0.5rem;">❌ ${ev.content}</div>`;
              }
            } catch (errParse) {}
            elements.aiMessagesContainer.scrollTop = elements.aiMessagesContainer.scrollHeight;
          }
        }
      }

      if (thoughtEl && !accumulatedContent) {
        thoughtEl.innerHTML = '';
      }

      // Save to chat history
      state.ai.chatHistory.push({ role: 'user', content: prompt });
      state.ai.chatHistory.push({ role: 'assistant', content: accumulatedContent });

    } catch (err) {
      if (contentEl) {
        contentEl.innerHTML = `<div class="alert alert-danger">Erro de execução da IA: ${err.message}</div>`;
      }
    } finally {
      state.ai.isGenerating = false;
      elements.btnSendAIMessage.disabled = false;
    }
  }

  function appendChatMessage(role, text) {
    if (!elements.aiMessagesContainer) return;
    const msg = document.createElement('div');
    msg.className = `chat-msg ${role}`;
    msg.innerHTML = `
      <div class="ai-avatar">${role === 'user' ? '👤' : '🤖'}</div>
      <div class="msg-bubble">${formatMarkdownText(text)}</div>
    `;
    elements.aiMessagesContainer.appendChild(msg);
    elements.aiMessagesContainer.scrollTop = elements.aiMessagesContainer.scrollHeight;
  }

  function renderActionProposalCard(proposal, containerEl) {
    if (!containerEl || !proposal) return;

    const card = document.createElement('div');
    card.className = 'ai-proposal-card';
    card.id = `proposal_${proposal.proposalId}`;

    const filesPreview = (proposal.files || []).slice(0, 10).map(f => {
      return `<div class="proposal-file-item" title="${f}">📄 ${f}</div>`;
    }).join('');

    card.innerHTML = `
      <div class="proposal-header">
        <span class="proposal-type-badge ${proposal.actionType.toLowerCase()}">⚡ PROPOSTA: ${proposal.actionType}</span>
        <span class="proposal-stats">Afeta ${proposal.fileCount} arquivos (${proposal.totalSize})</span>
      </div>
      <p style="font-size:0.85rem; margin-bottom:0.4rem;">${proposal.description || ''}</p>
      <div class="proposal-files-preview">
        ${filesPreview}
        ${proposal.fileCount > 10 ? `<div style="color:var(--accent-cyan); padding-top:4px;">... e mais ${proposal.fileCount - 10} arquivo(s)</div>` : ''}
      </div>
      <div class="proposal-actions">
        <button class="btn btn-secondary btn-sm btn-proposal-dryrun" data-prop-id="${proposal.proposalId}">
          🔍 Simulação Concluída (Dry-Run Seguro)
        </button>
        <button class="btn btn-danger btn-sm btn-proposal-execute" data-prop-id="${proposal.proposalId}" data-action="${proposal.actionType}">
          ⚡ Executar na Lixeira do Windows
        </button>
      </div>
    `;

    containerEl.appendChild(card);

    // Attach button listeners
    card.querySelector('.btn-proposal-dryrun').addEventListener('click', () => {
      showToast(`Simulação ativa: A proposta afetará ${proposal.fileCount} arquivos (${proposal.totalSize}) sem risco de perda antes de aprovação.`, 'info');
    });

    card.querySelector('.btn-proposal-execute').addEventListener('click', async () => {
      const confirmed = confirm(
        `CONFIRMAÇÃO DE AÇÃO:\n\n` +
        `Deseja executar a ação '${proposal.actionType}' para ${proposal.fileCount} arquivo(s) (${proposal.totalSize})?\n` +
        `Se for reciclagem, os arquivos irão para a Lixeira nativa do Windows.`
      );
      if (!confirmed) return;

      try {
        const res = await fetch('/api/ai/actions/execute', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            proposalId: proposal.proposalId,
            actionType: proposal.actionType,
            files: proposal.files,
          }),
        });

        if (!res.ok) throw new Error(await res.text());
        const result = await res.json();

        showToast(`Sucesso! ${result.message} (${result.freedSize} liberados)`, 'success');
        card.innerHTML = `
          <div style="color:#10b981; font-weight:700; font-size:0.9rem;">
            ✅ Ação executada com sucesso! ${result.message}
          </div>
        `;
        
        // Refresh duplicates & tree if active
        if (state.currentTab === 'duplicatesTab') loadDuplicates();
      } catch (err) {
        showToast('Erro ao executar proposta: ' + err.message, 'danger');
      }
    });
  }

  // Simple Markdown Parser for Text Formatting
  function formatMarkdownText(md) {
    if (!md) return '';
    let html = md
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;');

    // Fenced Code blocks
    html = html.replace(/```([a-zA-Z]*)\n([\s\S]*?)```/g, '<pre><code>$2</code></pre>');
    // Inline code
    html = html.replace(/`([^`]+)`/g, '<code>$1</code>');
    // Bold
    html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
    // Italic
    html = html.replace(/\*([^*]+)\*/g, '<em>$1</em>');
    // Line breaks
    html = html.replace(/\n\n/g, '<br><br>').replace(/\n/g, '<br>');

    return html;
  }

  // Initialize on DOM load
  document.addEventListener('DOMContentLoaded', init);
})();


