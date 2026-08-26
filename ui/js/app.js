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
    selectedFilesForDelete: new Map(), // Map<path, size>
    selectedIdleFiles: new Map(),      // Map<path, size>
    idleData: null,
    eventLogs: [],
    sseSource: null,
    uiZoom: 100,
  };

  // DOM Elements
  const elements = {
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
    btnStartScan: document.getElementById('btnStartScan'),
    btnCancelScan: document.getElementById('btnCancelScan'),
    progressHUD: document.getElementById('progressHUD'),
    hudPhaseBadge: document.getElementById('hudPhaseBadge'),
    hudErrorsBadge: document.getElementById('hudErrorsBadge'),
    statFiles: document.getElementById('statFiles'),
    statDirs: document.getElementById('statDirs'),
    statBytes: document.getElementById('statBytes'),
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
    treeTableBody: document.getElementById('treeTableBody'),
    treemapCurrentTitle: document.getElementById('treemapCurrentTitle'),
    treemapCurrentSubtitle: document.getElementById('treemapCurrentSubtitle'),
    btnResetZoom: document.getElementById('btnResetZoom'),
    treemapContainer: document.getElementById('treemapContainer'),
    treemapCanvas: document.getElementById('treemapCanvas'),
    treemapTooltip: document.getElementById('treemapTooltip'),
    treemapContextMenu: document.getElementById('treemapContextMenu'),
    ctxZoomIn: document.getElementById('ctxZoomIn'),
    ctxZoomOut: document.getElementById('ctxZoomOut'),
    ctxCopyPath: document.getElementById('ctxCopyPath'),
    ctxRecycle: document.getElementById('ctxRecycle'),
    treemapLegendBar: document.getElementById('treemapLegendBar'),
    // Duplicates elements
    dupCountBadge: document.getElementById('dupCountBadge'),
    dupTotalGroups: document.getElementById('dupTotalGroups'),
    dupTotalFiles: document.getElementById('dupTotalFiles'),
    dupTotalWasted: document.getElementById('dupTotalWasted'),
    dupSelectedCount: document.getElementById('dupSelectedCount'),
    dupSortBy: document.getElementById('dupSortBy'),
    dupMinSize: document.getElementById('dupMinSize'),
    dupSearch: document.getElementById('dupSearch'),
    btnSelectNewest: document.getElementById('btnSelectNewest'),
    btnSelectOldest: document.getElementById('btnSelectOldest'),
    btnClearSelection: document.getElementById('btnClearSelection'),
    btnRecycleSelected: document.getElementById('btnRecycleSelected'),
    duplicatesContainer: document.getElementById('duplicatesContainer'),
    // Folder duplicate & comparison elements
    folderDupCountBadge: document.getElementById('folderDupCountBadge'),
    dupFolderTotalGroups: document.getElementById('dupFolderTotalGroups'),
    dupFolderTotalCount: document.getElementById('dupFolderTotalCount'),
    dupFolderTotalWasted: document.getElementById('dupFolderTotalWasted'),
    dupFolderSortBy: document.getElementById('dupFolderSortBy'),
    dupFolderMinSize: document.getElementById('dupFolderMinSize'),
    dupFolderSearch: document.getElementById('dupFolderSearch'),
    btnRefreshFolderDuplicates: document.getElementById('btnRefreshFolderDuplicates'),
    folderDuplicatesContainer: document.getElementById('folderDuplicatesContainer'),
    comparePathA: document.getElementById('comparePathA'),
    comparePathB: document.getElementById('comparePathB'),
    btnSwapComparePaths: document.getElementById('btnSwapComparePaths'),
    btnRunFolderCompare: document.getElementById('btnRunFolderCompare'),
    folderCompareResults: document.getElementById('folderCompareResults'),
    // Idle / Stale files elements
    idleTotalCount: document.getElementById('idleTotalCount'),
    idleTotalBytes: document.getElementById('idleTotalBytes'),
    idleSelectedCount: document.getElementById('idleSelectedCount'),
    idleBucketsGrid: document.getElementById('idleBucketsGrid'),
    idleMinAge: document.getElementById('idleMinAge'),
    idleMinSize: document.getElementById('idleMinSize'),
    idleSearch: document.getElementById('idleSearch'),
    btnRefreshIdle: document.getElementById('btnRefreshIdle'),
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
    return new Intl.NumberFormat('pt-BR').format(num || 0);
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

  // Initialize
  async function init() {
    setupTabs();
    setupEventListeners();
    setupTreemap();
    setupCacheModals();
    setupFolderComparator();
    fetchPrivileges();
    await loadSavedConfig();
    fetchDrives();
    setupSSE();
    fetchEventLogs();
  }

  // Tabs Switching
  function setupTabs() {
    document.querySelectorAll('.nav-tab').forEach(btn => {
      btn.addEventListener('click', () => {
        const targetTab = btn.getAttribute('data-tab');
        document.querySelectorAll('.nav-tab').forEach(b => b.classList.remove('active'));
        document.querySelectorAll('.tab-pane').forEach(p => p.classList.remove('active'));
        
        btn.classList.add('active');
        document.getElementById(targetTab).classList.add('active');
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
        }
      });
    });
  }

  // Setup Event Listeners
  function setupEventListeners() {
    if (elements.btnElevateAdmin) {
      elements.btnElevateAdmin.addEventListener('click', elevateToAdmin);
    }
    elements.btnRefreshDrives.addEventListener('click', fetchDrives);
    
    elements.btnSelectAllDrives.addEventListener('click', () => {
      const allSelected = state.selectedRoots.size === state.drives.length;
      state.selectedRoots.clear();
      if (!allSelected) {
        state.drives.forEach(d => state.selectedRoots.add(d.letter));
      }
      renderDrivesGrid();
      saveCurrentConfig();
    });

    elements.btnStartScan.addEventListener('click', () => {
      saveCurrentConfig();
      startScan();
    });
    elements.btnCancelScan.addEventListener('click', cancelScan);

    // Save preferences on scan configuration change
    if (elements.workerThreads) elements.workerThreads.addEventListener('change', () => saveCurrentConfig());
    if (elements.hashAlgo) elements.hashAlgo.addEventListener('change', () => saveCurrentConfig());
    if (elements.hashMode) elements.hashMode.addEventListener('change', () => saveCurrentConfig());
    if (elements.minFileSize) elements.minFileSize.addEventListener('change', () => saveCurrentConfig());

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

    elements.dupSortBy.addEventListener('change', () => {
      loadDuplicates();
      saveCurrentConfig();
    });
    elements.dupMinSize.addEventListener('change', () => {
      loadDuplicates();
      saveCurrentConfig();
    });
    elements.dupSearch.addEventListener('input', debounce(loadDuplicates, 300));

    elements.btnSelectNewest.addEventListener('click', () => selectDuplicatesByStrategy('keep_newest'));
    elements.btnSelectOldest.addEventListener('click', () => selectDuplicatesByStrategy('keep_oldest'));
    elements.btnClearSelection.addEventListener('click', clearSelection);

    elements.btnRecycleSelected.addEventListener('click', recycleSelectedFiles);

    // Idle files listeners
    if (elements.idleMinAge) elements.idleMinAge.addEventListener('change', () => {
      loadIdleFiles();
      saveCurrentConfig();
    });
    if (elements.idleMinSize) elements.idleMinSize.addEventListener('change', () => {
      loadIdleFiles();
      saveCurrentConfig();
    });
    if (elements.idleSearch) elements.idleSearch.addEventListener('input', debounce(loadIdleFiles, 300));
    if (elements.btnRefreshIdle) elements.btnRefreshIdle.addEventListener('click', loadIdleFiles);
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
      loadFolderDuplicates();
      saveCurrentConfig();
    });
    if (elements.dupFolderMinSize) elements.dupFolderMinSize.addEventListener('change', () => {
      loadFolderDuplicates();
      saveCurrentConfig();
    });

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

    elements.btnClearLogs.addEventListener('click', () => {
      state.eventLogs = [];
      renderEventLogs();
      elements.eventCountBadge.textContent = '0';
    });
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

  // Load User Saved Configuration Preferences
  async function loadSavedConfig() {
    try {
      const res = await fetch('/api/config');
      if (!res.ok) return;
      const cfg = await res.json();
      if (!cfg) return;

      if (cfg.uiZoom) {
        setUIZoom(cfg.uiZoom, false);
      }

      if (cfg.workerThreads && elements.workerThreads) elements.workerThreads.value = String(cfg.workerThreads);
      if (cfg.hashAlgorithm && elements.hashAlgo) elements.hashAlgo.value = cfg.hashAlgorithm;
      if (cfg.hashMode && elements.hashMode) elements.hashMode.value = cfg.hashMode;
      if (cfg.minFileSize !== undefined && elements.minFileSize) elements.minFileSize.value = String(cfg.minFileSize);

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

      if (cfg.duplicatesSortBy && elements.dupSortBy) elements.dupSortBy.value = cfg.duplicatesSortBy;
      if (cfg.duplicatesMinSize !== undefined && elements.dupMinSize) elements.dupMinSize.value = String(cfg.duplicatesMinSize);

      if (cfg.idleMinAgeDays && elements.idleMinAge) elements.idleMinAge.value = String(cfg.idleMinAgeDays);
      if (cfg.idleMinSizeBytes !== undefined && elements.idleMinSize) elements.idleMinSize.value = String(cfg.idleMinSizeBytes);

      if (cfg.folderSortBy && elements.dupFolderSortBy) elements.dupFolderSortBy.value = cfg.folderSortBy;
      if (cfg.folderMinSize !== undefined && elements.dupFolderMinSize) elements.dupFolderMinSize.value = String(cfg.folderMinSize);

      if (cfg.selectedRoots && Array.isArray(cfg.selectedRoots) && cfg.selectedRoots.length > 0) {
        state.selectedRoots = new Set(cfg.selectedRoots);
      }
    } catch (e) {
      console.warn('Erro ao carregar preferências:', e);
    }
  }

  // Save User Configuration Preferences to scanfile_config.json
  async function saveCurrentConfig() {
    const payload = {
      selectedRoots: Array.from(state.selectedRoots),
      workerThreads: elements.workerThreads ? parseInt(elements.workerThreads.value, 10) : 8,
      hashAlgorithm: elements.hashAlgo ? elements.hashAlgo.value : 'xxhash',
      hashMode: elements.hashMode ? elements.hashMode.value : 'smart',
      minFileSize: elements.minFileSize ? parseInt(elements.minFileSize.value, 10) : 1,
      treemapDepth: state.treemap ? state.treemap.depth : 5,
      treemapColorMode: state.treemap ? state.treemap.colorMode : 'extension',
      treemapViewMode: state.treemap ? state.treemap.viewMode : 'split',
      duplicatesSortBy: elements.dupSortBy ? elements.dupSortBy.value : 'size_desc',
      duplicatesMinSize: elements.dupMinSize ? parseInt(elements.dupMinSize.value, 10) : 0,
      idleMinAgeDays: elements.idleMinAge ? parseInt(elements.idleMinAge.value, 10) : 365,
      idleMinSizeBytes: elements.idleMinSize ? parseInt(elements.idleMinSize.value, 10) : 104857600,
      folderSortBy: elements.dupFolderSortBy ? elements.dupFolderSortBy.value : 'wasted_desc',
      folderMinSize: elements.dupFolderMinSize ? parseInt(elements.dupFolderMinSize.value, 10) : 0,
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
  }

  // Debounce utility
  function debounce(func, wait) {
    let timeout;
    return function (...args) {
      clearTimeout(timeout);
      timeout = setTimeout(() => func.apply(this, args), wait);
    };
  }

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
    try {
      elements.drivesGrid.innerHTML = '<div class="loading-state">Carregando informações de discos...</div>';
      const res = await fetch('/api/drives');
      if (!res.ok) throw new Error('Falha ao obter discos');
      state.drives = await res.json();
      
      // Auto-select all fixed drives by default if none selected
      if (state.selectedRoots.size === 0) {
        state.drives.forEach(d => {
          if (d.driveType.includes('Fixed') || d.driveType.includes('Removable')) {
            state.selectedRoots.add(d.letter);
          }
        });
      }
      renderDrivesGrid();
    } catch (err) {
      elements.drivesGrid.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
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
            <input type="checkbox" class="drive-checkbox" ${isSelected ? 'checked' : ''} onclick="event.stopPropagation()">
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

  // Start Scan
  async function startScan() {
    if (state.selectedRoots.size === 0) {
      showToast('Por favor, selecione ao menos um disco para varrer!', 'danger');
      return;
    }

    const payload = {
      roots: Array.from(state.selectedRoots),
      workerThreads: parseInt(elements.workerThreads.value, 10),
      hashAlgorithm: elements.hashAlgo.value,
      hashAllFiles: elements.hashMode.value === 'all',
      minSizeForHash: parseInt(elements.minFileSize.value, 10),
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
      elements.btnCancelScan.classList.remove('hidden');
      elements.progressHUD.classList.remove('hidden');
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = 'Varrendo Discos...';

      showToast('Varredura multithread iniciada com sucesso!', 'success');
    } catch (err) {
      showToast('Erro ao iniciar varredura: ' + err.message, 'danger');
    }
  }

  // Cancel Scan
  async function cancelScan() {
    try {
      await fetch('/api/scan/cancel', { method: 'POST' });
      showToast('Varredura cancelada pelo usuário.', 'info');
    } catch (err) {
      console.error(err);
    }
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

    elements.statFiles.textContent = formatNumber(st.totalFilesScanned);
    elements.statDirs.textContent = formatNumber(st.totalDirsScanned);
    elements.statBytes.textContent = formatBytes(st.totalBytesScanned);
    elements.currentPathText.textContent = st.currentPath || 'Processando...';

    // Update Errors Badge
    if (st.errorsCount > 0) {
      elements.hudErrorsBadge.textContent = `⚠️ ${st.errorsCount} Itens Bloqueados / Ignorados`;
      elements.hudErrorsBadge.classList.remove('hidden');
    } else {
      elements.hudErrorsBadge.classList.add('hidden');
    }

    if (st.phase === 'phase1_metadata') {
      elements.hudPhaseBadge.textContent = 'Fase 1: Mapeamento de Metadados em Memória';
      elements.hudPhaseBadge.style.background = 'rgba(56, 189, 248, 0.15)';
      elements.hudPhaseBadge.style.color = '#38bdf8';
      elements.statSpeed.textContent = `${Math.round(st.scanRateFilesPerSec || 0)} arq/s`;
      elements.progressBarFill.style.width = '40%';
      elements.progressPercentText.textContent = 'Fase 1 em andamento';
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = 'Fase 1: Mapeando...';
    } else if (st.phase === 'phase2_hashing') {
      elements.hudPhaseBadge.textContent = 'Fase 2: Cálculo Concorrente de Hashes';
      elements.hudPhaseBadge.style.background = 'rgba(168, 85, 247, 0.15)';
      elements.hudPhaseBadge.style.color = '#a855f7';
      elements.statSpeed.textContent = `${(st.hashRateMBPerSec || 0).toFixed(1)} MB/s`;
      const pct = Math.min(100, Math.max(0, st.progressPercent || 0));
      elements.progressBarFill.style.width = `${pct}%`;
      elements.progressPercentText.textContent = `${pct.toFixed(1)}% (${formatNumber(st.filesHashedCount)} / ${formatNumber(st.filesToHashCount)})`;
      elements.liveBadge.className = 'status-badge scanning';
      elements.liveStatusText.textContent = 'Fase 2: Hashes...';
    } else if (st.phase === 'completed' || st.phase === 'watching') {
      elements.hudPhaseBadge.textContent = st.isWatching ? 'Sistema Conectado: Monitoramento em Tempo Real Ativo' : 'Varredura Concluída';
      elements.hudPhaseBadge.style.background = 'rgba(16, 185, 129, 0.15)';
      elements.hudPhaseBadge.style.color = '#10b981';
      elements.progressBarFill.style.width = '100%';
      elements.progressPercentText.textContent = '100%';
      elements.btnStartScan.classList.remove('hidden');
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
      // Extension / Type mode
      if (!node.isFile) {
        return LEVEL_COLORS[level % LEVEL_COLORS.length];
      }
      const ext = (node.name || '').split('.').pop().toLowerCase();
      const type = EXT_MAP[ext] || 'other';
      return FILE_TYPE_COLORS[type] || FILE_TYPE_COLORS.other;
    }
  }

  // Load Tree and Treemap Data from Backend
  async function loadTreeData(path = '') {
    state.treePath = path;
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
      items = state.treeData;
      parentSize = items.reduce((acc, cur) => acc + (cur.totalSize || 0), 0) || 1;
    } else {
      parentSize = state.treeData.totalSize || 1;
      if (state.treeData.subDirs) items.push(...state.treeData.subDirs);
      if (state.treeData.files) {
        items.push(...state.treeData.files.map(f => ({
          path: f.path,
          name: f.name,
          totalSize: f.size,
          fileCount: 1,
          subDirCount: 0,
          createTime: f.createTime,
          modTime: f.modTime,
          isFile: true,
        })));
      }
    }

    const search = elements.treeSearchInput ? elements.treeSearchInput.value.toLowerCase().trim() : '';
    if (search) {
      items = items.filter(it => it.name.toLowerCase().includes(search));
    }

    if (items.length === 0) {
      elements.treeTableBody.innerHTML = '<tr><td colspan="7" class="empty-state">Nenhum item nesta pasta.</td></tr>';
      return;
    }

    elements.treeTableBody.innerHTML = items.map(item => {
      const pct = Math.min(100, ((item.totalSize / parentSize) * 100)).toFixed(1);
      const isDir = !item.isFile;

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

  // Squarified Treemap Layout Computation (Bruls, Huizing, van Wijk algorithm)
  function computeSquarifiedLayout(container, x, y, width, height, level, maxDepth) {
    if (width <= 0 || height <= 0 || !container) return [];
    const results = [];

    const isRoot = level === 0;

    // Collect children
    let children = [];
    if (container.subDirs && container.subDirs.length > 0) {
      children.push(...container.subDirs.map(d => ({
        name: d.name,
        path: d.path,
        totalSize: d.totalSize || 0,
        fileCount: d.fileCount || 0,
        subDirCount: d.subDirCount || 0,
        modTime: d.modTime,
        createTime: d.createTime,
        isFile: false,
        subDirs: d.subDirs,
        files: d.files,
      })));
    }
    if (container.files && container.files.length > 0) {
      children.push(...container.files.map(f => ({
        name: f.name,
        path: f.path,
        totalSize: f.size || 0,
        fileCount: 1,
        subDirCount: 0,
        modTime: f.modTime,
        createTime: f.createTime,
        isFile: true,
      })));
    }

    // Filter out 0-byte items
    children = children.filter(c => c.totalSize > 0);
    children.sort((a, b) => b.totalSize - a.totalSize);

    const totalChildSize = children.reduce((acc, c) => acc + c.totalSize, 0);

    if (children.length === 0 || level >= maxDepth || totalChildSize <= 0) {
      results.push({
        x, y, w: width, h: height,
        node: container,
        level,
        isLeaf: true,
      });
      return results;
    }

    // Header margin if directory has children
    const headerHeight = (width > 90 && height > 70 && !isRoot) ? 18 : 0;
    const padding = 2;

    if (!isRoot) {
      results.push({
        x, y, w: width, h: height,
        node: container,
        level,
        isLeaf: false,
        headerHeight,
      });
    }

    const availX = x + padding;
    const availY = y + headerHeight + padding;
    const availW = width - (padding * 2);
    const availH = height - headerHeight - (padding * 2);

    if (availW < 3 || availH < 3) return results;

    const rects = squarify(children, totalChildSize, availX, availY, availW, availH);

    rects.forEach(item => {
      if (item.node.isFile || level + 1 >= maxDepth) {
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
        const subResults = computeSquarifiedLayout(item.node, item.x, item.y, item.w, item.h, level + 1, maxDepth);
        results.push(...subResults);
      }
    });

    return results;
  }

  function squarify(children, totalSize, x, y, width, height) {
    const rects = [];
    let remaining = [...children];
    let curX = x;
    let curY = y;
    let curW = width;
    let curH = height;

    while (remaining.length > 0) {
      const isHorizontal = curW >= curH;
      const side = isHorizontal ? curH : curW;

      let row = [remaining[0]];
      let bestWorst = worstRatio(row, side, totalSize, isHorizontal ? curW : curH);
      let i = 1;

      while (i < remaining.length) {
        const testRow = [...row, remaining[i]];
        const testWorst = worstRatio(testRow, side, totalSize, isHorizontal ? curW : curH);
        if (testWorst <= bestWorst) {
          bestWorst = testWorst;
          row = testRow;
          i++;
        } else {
          break;
        }
      }

      const rowSum = row.reduce((acc, c) => acc + c.totalSize, 0);
      const rowThickness = totalSize > 0 ? (rowSum / totalSize) * (isHorizontal ? curW : curH) : 0;

      let offset = 0;
      for (const item of row) {
        const itemLen = rowSum > 0 ? (item.totalSize / rowSum) * side : 0;
        if (isHorizontal) {
          rects.push({
            x: curX,
            y: curY + offset,
            w: Math.max(0, rowThickness),
            h: Math.max(0, itemLen),
            node: item,
          });
        } else {
          rects.push({
            x: curX + offset,
            y: curY,
            w: Math.max(0, itemLen),
            h: Math.max(0, rowThickness),
            node: item,
          });
        }
        offset += itemLen;
      }

      if (isHorizontal) {
        curX += rowThickness;
        curW -= rowThickness;
      } else {
        curY += rowThickness;
        curH -= rowThickness;
      }

      remaining = remaining.slice(row.length);
    }

    return rects;
  }

  function worstRatio(row, side, totalSize, totalLength) {
    if (row.length === 0 || side <= 0 || totalSize <= 0 || totalLength <= 0) return Infinity;
    const rowSum = row.reduce((acc, c) => acc + c.totalSize, 0);
    const rowThickness = (rowSum / totalSize) * totalLength;
    if (rowThickness <= 0) return Infinity;

    let maxRatio = 0;
    for (const item of row) {
      const itemLen = (item.totalSize / rowSum) * side;
      if (itemLen <= 0) return Infinity;
      const ratio = Math.max(itemLen / rowThickness, rowThickness / itemLen);
      if (ratio > maxRatio) maxRatio = ratio;
    }
    return maxRatio;
  }

  // Render Treemap on Canvas with Cushion Shading
  function renderTreemapCanvas() {
    if (!elements.treemapCanvas) return;
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

    // Draw nodes: First containers/parents, then leaves
    const nonLeaves = state.treemap.layoutNodes.filter(n => !n.isLeaf);
    const leaves = state.treemap.layoutNodes.filter(n => n.isLeaf);

    // Draw container header titles
    nonLeaves.forEach(n => {
      if (n.headerHeight && n.headerHeight > 0) {
        ctx.fillStyle = 'rgba(15, 23, 42, 0.7)';
        ctx.fillRect(n.x, n.y, n.w, n.headerHeight);
        ctx.strokeStyle = 'rgba(56, 189, 248, 0.25)';
        ctx.lineWidth = 1;
        ctx.strokeRect(n.x, n.y, n.w, n.headerHeight);

        if (n.w > 60) {
          ctx.save();
          ctx.beginPath();
          ctx.rect(n.x + 2, n.y, n.w - 4, n.headerHeight);
          ctx.clip();
          ctx.fillStyle = '#94a3b8';
          ctx.font = '600 10px Inter, sans-serif';
          ctx.fillText(`📁 ${n.node.name} (${formatBytes(n.node.totalSize)})`, n.x + 4, n.y + 12);
          ctx.restore();
        }
      }
    });

    // Draw leaf cushions
    leaves.forEach(n => {
      const isHovered = state.treemap.hoveredNode && state.treemap.hoveredNode.node.path === n.node.path;
      const isSelected = state.treemap.selectedNode && state.treemap.selectedNode.node.path === n.node.path;
      const baseColor = getNodeColor(n.node, colorMode, n.level);

      const label = n.node.name;
      const sublabel = formatBytes(n.node.totalSize);

      drawCushionRect(ctx, n.x, n.y, n.w, n.h, baseColor, isHovered, isSelected, label, sublabel, n.isLeaf, n.level);
    });
  }

  // Draw Cushion Shading & Gradient Borders
  function drawCushionRect(ctx, x, y, w, h, baseColor, isHovered, isSelected, label, sublabel, isLeaf, level) {
    if (w <= 0 || h <= 0) return;

    // Fill Base Color
    ctx.fillStyle = baseColor;
    ctx.fillRect(x, y, w, h);

    // Cushion Surface Gradient
    const grad = ctx.createLinearGradient(x, y, x + w, y + h);
    grad.addColorStop(0, 'rgba(255, 255, 255, 0.22)');
    grad.addColorStop(0.4, 'rgba(255, 255, 255, 0.03)');
    grad.addColorStop(1, 'rgba(0, 0, 0, 0.45)');
    ctx.fillStyle = grad;
    ctx.fillRect(x, y, w, h);

    // Border
    if (isSelected) {
      ctx.strokeStyle = '#f59e0b';
      ctx.lineWidth = 2.5;
    } else if (isHovered) {
      ctx.strokeStyle = '#38bdf8';
      ctx.lineWidth = 2;
    } else {
      ctx.strokeStyle = 'rgba(0, 0, 0, 0.7)';
      ctx.lineWidth = 1;
    }
    ctx.strokeRect(x, y, w, h);

    if (isHovered) {
      ctx.fillStyle = 'rgba(56, 189, 248, 0.25)';
      ctx.fillRect(x, y, w, h);
    }

    // Text Label (clipped inside rectangle)
    if (w > 40 && h > 18 && label) {
      ctx.save();
      ctx.beginPath();
      ctx.rect(x + 2, y + 2, Math.max(0, w - 4), Math.max(0, h - 4));
      ctx.clip();

      ctx.fillStyle = '#ffffff';
      ctx.font = 'bold 11px Inter, sans-serif';
      ctx.shadowColor = 'rgba(0, 0, 0, 0.9)';
      ctx.shadowBlur = 4;
      ctx.fillText(label, x + 4, y + 13);

      if (h > 32 && sublabel) {
        ctx.fillStyle = 'rgba(255, 255, 255, 0.85)';
        ctx.font = '10px "JetBrains Mono", monospace';
        ctx.shadowBlur = 3;
        ctx.fillText(sublabel, x + 4, y + 26);
      }

      ctx.restore();
    }
  }

  // Render Treemap Legend Bar (Chips at bottom of chart)
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
      // Extension
      const types = [
        { color: FILE_TYPE_COLORS.video, label: '🎬 Vídeos' },
        { color: FILE_TYPE_COLORS.image, label: '🖼️ Imagens' },
        { color: FILE_TYPE_COLORS.audio, label: '🎵 Áudio' },
        { color: FILE_TYPE_COLORS.archive, label: '📦 Compactados' },
        { color: FILE_TYPE_COLORS.executable, label: '⚙️ Executáveis' },
        { color: FILE_TYPE_COLORS.document, label: '📄 Documentos' },
        { color: FILE_TYPE_COLORS.code, label: '💻 Código / DB' },
        { color: FILE_TYPE_COLORS.folder, label: '📁 Pastas' },
        { color: FILE_TYPE_COLORS.other, label: '📦 Outros' },
      ];
      elements.treemapLegendBar.innerHTML = types.map(t => `
        <div class="legend-chip">
          <span class="legend-color-dot" style="background:${t.color}"></span>
          <span>${t.label}</span>
        </div>
      `).join('');
    }
  }

  // Load Duplicate Groups
  async function loadDuplicates() {
    const sortBy = elements.dupSortBy.value;
    const minSize = elements.dupMinSize.value;
    const search = elements.dupSearch.value.trim();

    try {
      const url = `/api/duplicates?sortBy=${sortBy}&minSize=${minSize}&search=${encodeURIComponent(search)}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Falha ao carregar duplicados');
      const data = await res.json();
      state.duplicatesData = data;
      renderDuplicates(data);
    } catch (err) {
      elements.duplicatesContainer.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
    }
  }

  // Render Duplicates Cards
  function renderDuplicates(data) {
    if (!data || !data.groups || data.groups.length === 0) {
      elements.duplicatesContainer.innerHTML = '<div class="empty-state">Nenhum arquivo duplicado encontrado com os filtros atuais.</div>';
      return;
    }

    elements.dupTotalGroups.textContent = formatNumber(data.totalGroups);
    elements.dupTotalFiles.textContent = formatNumber(data.totalFiles);
    elements.dupTotalWasted.textContent = formatBytes(data.wastedBytes);

    elements.duplicatesContainer.innerHTML = data.groups.map(grp => {
      const hashShort = grp.hash.length > 22 ? grp.hash.substring(0, 22) + '...' : grp.hash;

      return `
        <div class="dup-group-card" data-group-id="${grp.id}">
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
            ${grp.files.map((file, idx) => {
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
        </div>
      `;
    }).join('');

    // Attach checkbox handlers
    elements.duplicatesContainer.querySelectorAll('.dup-file-checkbox').forEach(cb => {
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

    // Group button handler
    elements.duplicatesContainer.querySelectorAll('.btn-group-keep-newest').forEach(btn => {
      btn.addEventListener('click', () => {
        const gId = btn.getAttribute('data-group-id');
        const grp = data.groups.find(g => g.id === gId);
        if (!grp || grp.files.length < 2) return;

        // Files are sorted by ModTime ascending (last file is newest)
        const newest = grp.files[grp.files.length - 1];
        grp.files.forEach(f => {
          if (f.path !== newest.path) {
            state.selectedFilesForDelete.set(f.path, f.size);
          } else {
            state.selectedFilesForDelete.delete(f.path);
          }
        });
        renderDuplicates(state.duplicatesData);
        updateSelectionSummary();
      });
    });

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
      showToast(`Carregando cache de ${filePath}...`, 'info');
      const res = await fetch('/api/cache/load', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ filePath }),
      });

      if (!res.ok) throw new Error(await res.text());
      const data = await res.json();

      elements.loadCacheModal.classList.add('hidden');
      showToast(`Cache carregado! ${formatNumber(data.snapshot.totalFiles)} arquivos (${formatBytes(data.snapshot.totalBytes)}) restaurados em memória.`, 'success');

      // Update UI displays
      elements.statFiles.textContent = formatNumber(data.snapshot.totalFiles);
      elements.statDirs.textContent = formatNumber(data.snapshot.totalDirs);
      elements.statBytes.textContent = formatBytes(data.snapshot.totalBytes);
      elements.currentPathText.textContent = `Snapshot carregado: ${data.snapshot.fileName || filePath}`;

      // Refresh other tabs
      loadDuplicates();
      loadFolderDuplicates();
      loadTreeData();
    } catch (err) {
      showToast(`Erro ao carregar cache: ${err.message}`, 'danger');
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

    if (elements.dupFolderSortBy) elements.dupFolderSortBy.addEventListener('change', loadFolderDuplicates);
    if (elements.dupFolderMinSize) elements.dupFolderMinSize.addEventListener('change', loadFolderDuplicates);
    if (elements.dupFolderSearch) elements.dupFolderSearch.addEventListener('input', debounce(loadFolderDuplicates, 300));
    if (elements.btnRefreshFolderDuplicates) elements.btnRefreshFolderDuplicates.addEventListener('click', loadFolderDuplicates);

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
  async function loadFolderDuplicates() {
    if (!elements.folderDuplicatesContainer) return;

    const sortBy = elements.dupFolderSortBy.value;
    const minSize = elements.dupFolderMinSize.value;
    const search = elements.dupFolderSearch.value.trim();

    try {
      const url = `/api/folders/duplicates?sortBy=${sortBy}&minSize=${minSize}&search=${encodeURIComponent(search)}`;
      const res = await fetch(url);
      if (!res.ok) throw new Error('Falha ao obter pastas duplicadas');
      const data = await res.json();
      state.folderDuplicatesData = data;
      renderFolderDuplicates(data);
    } catch (err) {
      elements.folderDuplicatesContainer.innerHTML = `<div class="empty-state">Erro: ${err.message}</div>`;
    }
  }

  // Render Duplicate Folders
  function renderFolderDuplicates(data) {
    if (!data || !data.groups || data.groups.length === 0) {
      elements.folderDuplicatesContainer.innerHTML = '<div class="empty-state">Nenhuma pasta duplicada encontrada com os filtros atuais. Realize uma varredura para identificar pastas com conteúdo idêntico.</div>';
      return;
    }

    elements.dupFolderTotalGroups.textContent = formatNumber(data.totalGroups);
    elements.dupFolderTotalCount.textContent = formatNumber(data.totalFolders);
    elements.dupFolderTotalWasted.textContent = formatBytes(data.wastedBytes);

    elements.folderDuplicatesContainer.innerHTML = data.groups.map(grp => {
      const hashShort = grp.folderHash.length > 24 ? grp.folderHash.substring(0, 24) + '...' : grp.folderHash;

      return `
        <div class="dup-group-card folder-dup-group-card" data-group-id="${grp.id}">
          <div class="dup-group-header">
            <div class="dup-group-left">
              <span class="dup-hash-pill" title="${grp.folderHash}">📁 ${hashShort}</span>
              <span class="dup-size-pill">${formatBytes(grp.folderSize)} por pasta</span>
              <span class="dup-size-pill">📄 ${formatNumber(grp.fileCount)} arquivos cada</span>
              <span class="dup-wasted-badge">Desperdício: ${formatBytes(grp.wastedBytes)} (${grp.folderCount} cópias da pasta)</span>
            </div>
            <div class="dup-group-actions">
              ${grp.folders.length >= 2 ? `
                <button class="btn btn-secondary btn-sm btn-quick-compare-folders" 
                        data-path-a="${grp.folders[0].path}" 
                        data-path-b="${grp.folders[1].path}">
                  ⚖️ Comparar Pasta 1 e 2
                </button>
              ` : ''}
            </div>
          </div>

          <div class="dup-file-list">
            ${grp.folders.map((folder, idx) => {
              return `
                <div class="dup-file-row">
                  <div class="dup-file-left">
                    <span class="folder-badge-num">#${idx + 1}</span>
                    <span class="dup-file-path truncate" title="${folder.path}">${folder.path}</span>
                  </div>
                  <div class="dup-file-date">
                    <span>${formatNumber(folder.fileCount)} arq</span> &bull; 
                    <strong>${formatBytes(folder.totalSize)}</strong>
                  </div>
                </div>
              `;
            }).join('')}
          </div>
        </div>
      `;
    }).join('');

    // Attach quick compare button handlers
    elements.folderDuplicatesContainer.querySelectorAll('.btn-quick-compare-folders').forEach(btn => {
      btn.addEventListener('click', () => {
        const pA = btn.getAttribute('data-path-a');
        const pB = btn.getAttribute('data-path-b');
        elements.comparePathA.value = pA;
        elements.comparePathB.value = pB;

        // Switch to compare subtab
        const compTabBtn = document.querySelector('.folder-subtab[data-subtab="folderCompareSubtab"]');
        if (compTabBtn) compTabBtn.click();
        runFolderCompare();
      });
    });
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
  async function loadIdleFiles() {
    if (!elements.idleTableBody) return;

    const minAge = elements.idleMinAge ? elements.idleMinAge.value : 365;
    const minSize = elements.idleMinSize ? elements.idleMinSize.value : 104857600;
    const search = elements.idleSearch ? elements.idleSearch.value.trim() : '';

    try {
      elements.idleTableBody.innerHTML = '<tr><td colspan="6" class="loading-state">Analisando datas de modificação e calculando idades dos arquivos...</td></tr>';
      const url = `/api/stats/idle-files?minAgeDays=${minAge}&minSize=${minSize}&search=${encodeURIComponent(search)}`;
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

    if (elements.idleTotalCount) elements.idleTotalCount.textContent = formatNumber(data.totalIdleFiles);
    if (elements.idleTotalBytes) elements.idleTotalBytes.textContent = formatBytes(data.totalIdleBytes);

    // Render Age Buckets Grid
    if (elements.idleBucketsGrid && data.ageBuckets) {
      elements.idleBucketsGrid.innerHTML = data.ageBuckets.map(b => `
        <div class="idle-bucket-card">
          <div class="idle-bucket-label">${b.label}</div>
          <div class="idle-bucket-size">${formatBytes(b.totalBytes)}</div>
          <div class="idle-bucket-count">${formatNumber(b.fileCount)} arquivos</div>
        </div>
      `).join('');
    }

    if (!data.topFiles || data.topFiles.length === 0) {
      elements.idleTableBody.innerHTML = '<tr><td colspan="6" class="empty-state">Nenhum arquivo ocioso encontrado com os filtros atuais.</td></tr>';
      updateIdleSelectionSummary();
      return;
    }

    elements.idleTableBody.innerHTML = data.topFiles.map(file => {
      const isMarked = state.selectedIdleFiles.has(file.path);
      const years = (file.inactiveDays / 365.25).toFixed(1);
      const ageLabel = file.inactiveDays >= 365 ? `${years} anos (${formatNumber(file.inactiveDays)} dias)` : `${formatNumber(file.inactiveDays)} dias`;

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

  // Initialize on DOM load
  document.addEventListener('DOMContentLoaded', init);
})();

