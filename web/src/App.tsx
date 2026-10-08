import { useCallback, useEffect, useMemo, useRef, useState } from 'preact/hooks';
import { InvalidSession, Main, Sidebar, StatusBar, TopBar, type Screen } from './components/Shell';
import { api, ApiError, SessionExpiredError } from './lib/api';
import { formatBytes } from './lib/format';
import type {
  Config, ExportView, LogLine, Overview, Progress, ResultCategory, Status, Volume,
} from './lib/types';
import { OverviewScreen } from './screens/OverviewScreen';
import { ExportScreen } from './screens/ExportScreen';
import { PasteScreen } from './screens/PasteScreen';
import { ResultsScreen } from './screens/ResultsScreen';
import { DeleteScreen } from './screens/DeleteScreen';
import { HistoryScreen } from './screens/HistoryScreen';
import { pruneSelection } from './lib/results';
import { RedactScreen } from './screens/RedactScreen';
import { RulesScreen } from './screens/RulesScreen';
import { ScanningScreen } from './screens/ScanningScreen';
import { ScanScreen, type ScanSelection } from './screens/ScanScreen';

type Theme = 'dark' | 'light';

const heartbeatMilliseconds = 60_000;

function preferredTheme(config?: Config): Theme {
  if (config?.theme === 'dark' || config?.theme === 'light') return config.theme;
  return window.matchMedia?.('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
}

export function App() {
  const [status, setStatus] = useState<Status | null>(null);
  const [invalid, setInvalid] = useState(false);
  const [screen, setScreen] = useState<Screen>('scan');
  const [theme, setTheme] = useState<Theme>(preferredTheme());
  const [config, setConfig] = useState<Config>({ theme: '', includeOtherProfiles: false,
    exclusions: [] });
  const [volumes, setVolumes] = useState<Volume[]>([]);
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [progress, setProgress] = useState<Progress | null>(null);
  const [log, setLog] = useState<LogLine[]>([]);
  const [overview, setOverview] = useState<Overview | null>(null);
  const [error, setError] = useState('');
  const [batch, setBatch] = useState(1);
  const [exportCode, setExportCode] = useState('');
  const onExportLoaded = useCallback((view: ExportView) => setExportCode(view.code), []);
  // Selected result items, kept here so step 7 can pick them up.
  const [deletionSelection, setDeletionSelection] = useState<Set<string>>(new Set());
  const onResultsLoaded = useCallback((categories: ResultCategory[]) =>
    setDeletionSelection((previous) => pruneSelection(categories, previous)), []);
  const [resultsRefresh, setResultsRefresh] = useState(0);
  // Unparsed replies per step 5 round, so leaving the step does not lose them.
  const [pasteDrafts, setPasteDrafts] = useState<Record<number, string>>({});
  const onDraftChange = useCallback((round: number, text: string) =>
    setPasteDrafts((previous) => ({ ...previous, [round]: text })), []);
  const appElement = useRef<HTMLDivElement>(null);

  // Any rejected session (wrong or missing token) switches to the invalid-link state.
  const guard = useCallback(async <T,>(work: Promise<T>): Promise<T | undefined> => {
    try {
      return await work;
    } catch (failure) {
      if (failure instanceof SessionExpiredError) setInvalid(true);
      else setError(failure instanceof ApiError ? failure.message : String(failure));
      return undefined;
    }
  }, []);

  const loadOverview = useCallback(async () => {
    const loaded = await guard(api.get<Overview>('/api/overview'));
    if (loaded) {
      setOverview(loaded);
      setScreen('overview');
    }
  }, [guard]);

  useEffect(() => {
    document.documentElement.dataset.theme = theme;
  }, [theme]);

  useEffect(() => {
    (async () => {
      const loadedStatus = await guard(api.get<Status>('/api/status'));
      if (!loadedStatus) return;
      setStatus(loadedStatus);
      setConfig(loadedStatus.config);
      setTheme(preferredTheme(loadedStatus.config));
      const loadedVolumes = (await guard(api.get<Volume[]>('/api/volumes'))) ?? [];
      setVolumes(loadedVolumes);
      const systemVolume = loadedVolumes.find((volume) => /^c:/i.test(volume.name)) ??
        loadedVolumes[0];
      if (systemVolume) setSelected(new Set([systemVolume.root]));
      if (loadedStatus.state === 'running') setScreen('scanning');
      else if (loadedStatus.hasOverview) await loadOverview();
    })();
    const heartbeat = window.setInterval(
      () => guard(api.post('/api/heartbeat')), heartbeatMilliseconds);
    return () => window.clearInterval(heartbeat);
  }, [guard, loadOverview]);

  // While scanning, progress arrives over Server-Sent Events (cookie-authenticated).
  useEffect(() => {
    if (screen !== 'scanning') return;
    const source = new EventSource('/api/scan/stream');
    source.addEventListener('progress', (event) => {
      const update = JSON.parse((event as MessageEvent<string>).data) as Progress;
      setProgress(update);
      if (update.log.length) setLog((previous) => [...previous, ...update.log].slice(-400));
      if (update.state === 'finished') {
        source.close();
        setStatus((previous) => previous && { ...previous, sessionCode: update.sessionCode,
          hasOverview: true, firstRun: false });
        void loadOverview();
      } else if (update.state === 'cancelled' || update.state === 'failed') {
        source.close();
        setError(update.state === 'failed' ? `Scan failed: ${update.failure ?? ''}` :
          'Scan cancelled. Nothing was changed.');
        setScreen(overview ? 'overview' : 'scan');
      }
    });
    source.onerror = () => {
      // The stream also fails when the session is no longer valid; ask the API to tell.
      void guard(api.get<Status>('/api/status'));
    };
    return () => source.close();
  }, [screen, guard, loadOverview, overview]);

  const startScan = useCallback(async (selection: ScanSelection) => {
    setError('');
    setLog([]);
    setProgress(null);
    const started = await guard(api.post<Progress>('/api/scan', selection));
    if (started) {
      setProgress({ ...started, log: [] });
      setStatus((previous) => previous && { ...previous, sessionCode: started.sessionCode });
      setScreen('scanning');
    }
  }, [guard]);

  const updateConfig = useCallback((next: Config) => setConfig(next), []);

  const toggleTheme = useCallback(() => {
    const next: Theme = theme === 'dark' ? 'light' : 'dark';
    setTheme(next);
    void guard(api.post('/api/config', { theme: next }));
  }, [theme, guard]);

  const relaunch = useCallback(async () => {
    const result = await guard(api.post('/api/relaunch-elevated'));
    if (result) setError('Relaunching as administrator. A new tab will open; close this one.');
  }, [guard]);

  const reachedStep = overview ? (deletionSelection.size > 0 ? 7 : 6) : 1;

  const navigate = useCallback((target: Screen) => {
    if (target === 'scan' && screen === 'scanning') return;
    const needsScan: Screen[] = ['overview', 'redact', 'export', 'paste', 'results', 'delete'];
    if (needsScan.includes(target) && !overview) return;
    if (target === 'delete' && deletionSelection.size === 0) return;
    if ([...needsScan, 'scan', 'rules', 'history'].includes(target)) setScreen(target);
  }, [screen, overview, deletionSelection]);

  const startFromKeyboard = useCallback(() => {
    const roots = volumes.filter((volume) => selected.has(volume.root)).map((v) => v.root);
    if (roots.length) {
      void startScan({ roots, includeOtherProfiles: config.includeOtherProfiles,
        exclusions: config.exclusions });
    }
  }, [volumes, selected, config, startScan]);

  const onKeyDown = useCallback((event: KeyboardEvent) => {
    const target = event.target as HTMLElement;
    if (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA') return;
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === '1') navigate('scan');
    else if (event.key === '2') navigate('overview');
    else if (event.key === '3') navigate('redact');
    else if (event.key === '4') navigate('export');
    else if (event.key === '5') navigate('paste');
    else if (event.key === '6') navigate('results');
    else if (event.key === '7') navigate('delete');
    else if (event.key === 'h' || event.key === 'H') navigate('history');
    else if (event.key === 'r' || event.key === 'R') navigate('rules');
    else if (event.key === 't' || event.key === 'T') toggleTheme();
    else if (event.key === 'Enter' && screen === 'scan' && target.tagName !== 'BUTTON') {
      event.preventDefault();
      startFromKeyboard();
    } else if (event.key === 'Escape' && screen === 'scanning') {
      void guard(api.post('/api/scan/cancel'));
    }
  }, [navigate, toggleTheme, screen, startFromKeyboard, guard]);

  useEffect(() => {
    window.addEventListener('keydown', onKeyDown);
    return () => window.removeEventListener('keydown', onKeyDown);
  }, [onKeyDown]);

  const statusVolume = useMemo(() => {
    const root = overview?.roots.find((candidate) => candidate.isVolumeRoot);
    if (root) {
      return { name: root.display, usedBytes: root.usedBytes, totalBytes: root.totalBytes,
        freeBytes: root.freeBytes };
    }
    const volume = volumes.find((candidate) => selected.has(candidate.root));
    return volume && { name: volume.name, usedBytes: volume.usedBytes,
      totalBytes: volume.totalBytes, freeBytes: volume.freeBytes };
  }, [overview, volumes, selected]);

  if (invalid) {
    return (
      <div class="app">
        <InvalidSession port={status?.port ?? Number(window.location.port)} />
      </div>
    );
  }
  if (!status) return <div class="app" />;

  const crumbs: Record<Screen, string> = {
    scan: 'Scan', scanning: 'Scanning', overview: 'Overview', redact: 'Redaction',
    export: 'Export prompt', paste: 'Paste response', results: 'Results', delete: 'Delete',
    rules: 'Redaction rules', history: 'History',
  };
  const scanMeta = screen === 'scanning' && progress?.expectedBytes
    ? `${Math.floor(Math.min(0.99, progress.allocatedBytes / progress.expectedBytes) * 100)}%`
    : overview?.roots.map((root) => root.display).join(' ') ?? '';

  return (
    <div class="app" ref={appElement}>
      <TopBar
        version={status.version}
        sessionCode={exportCode || progress?.sessionCode || overview?.sessionCode ||
          status.sessionCode}
        crumb={crumbs[screen]}
        elevated={status.elevated}
        theme={theme}
        onToggleTheme={toggleTheme}
      />
      <div class="body">
        <Sidebar
          active={screen}
          reachedStep={reachedStep}
          meta={{ scan: scanMeta, overview: overview ? formatBytes(overview.totals.used) : '' }}
          port={status.port}
          onNavigate={navigate}
        />
        <Main>
          {screen === 'scan' && (
            <ScanScreen
              volumes={volumes}
              config={config}
              elevated={status.elevated}
              canRelaunch={status.canRelaunch}
              firstRun={status.firstRun && !overview}
              platform={status.platform}
              lastScan={overview ? { at: overview.scannedAt,
                roots: overview.roots.map((root) => root.display).join(' '),
                usedBytes: overview.totals.used } : undefined}
              error={error}
              selected={selected}
              onToggleVolume={(root) => setSelected((previous) => {
                const next = new Set(previous);
                if (next.has(root)) next.delete(root);
                else next.add(root);
                return next;
              })}
              onStart={startScan}
              onRelaunch={relaunch}
              onConfigChange={updateConfig}
            />
          )}
          {screen === 'scanning' && progress && (
            <ScanningScreen
              progress={progress}
              log={log}
              onPause={() => guard(api.post('/api/scan/pause'))}
              onResume={() => guard(api.post('/api/scan/resume'))}
              onCancel={() => guard(api.post('/api/scan/cancel'))}
            />
          )}
          {screen === 'overview' && overview && (
            <OverviewScreen
              overview={overview}
              canRelaunch={status.canRelaunch}
              onRescan={() => { setError(''); setScreen('scan'); }}
              onRelaunch={relaunch}
              onReview={() => setScreen('redact')}
            />
          )}
          {screen === 'redact' && (
            <RedactScreen guard={guard} onOpenRules={() => setScreen('rules')}
              onContinue={() => setScreen('export')} />
          )}
          {screen === 'rules' && <RulesScreen guard={guard} />}
          {screen === 'history' && <HistoryScreen guard={guard} />}
          {screen === 'export' && (
            <ExportScreen guard={guard} batch={batch} onBatchChange={setBatch}
              onGoPaste={() => setScreen('paste')} onExportLoaded={onExportLoaded} />
          )}
          {screen === 'paste' && (
            <PasteScreen guard={guard} batch={batch} onBatchChange={setBatch}
              onGoExport={() => setScreen('export')}
              onGoResults={() => setScreen('results')}
              drafts={pasteDrafts} onDraftChange={onDraftChange} />
          )}
          {screen === 'results' && (
            <ResultsScreen guard={guard} selection={deletionSelection}
              onSelectionChange={setDeletionSelection} onResultsLoaded={onResultsLoaded}
              onGoExport={(round) => { setBatch(round); setScreen('export'); }}
              onRescan={() => { setError(''); setScreen('scan'); }}
              onReviewDeletion={() => setScreen('delete')} refreshToken={resultsRefresh} />
          )}
          {screen === 'delete' && (
            <DeleteScreen guard={guard} keys={[...deletionSelection]}
              volumeName={statusVolume?.name ?? 'this drive'}
              onDone={(changed) => {
                setScreen('results');
                if (changed) setResultsRefresh((previous) => previous + 1);
              }} />
          )}
        </Main>
      </div>
      <StatusBar
        volumeSummary={statusVolume}
        files={screen === 'scanning' ? progress?.files : overview?.files}
        elevated={status.elevated}
      />
    </div>
  );
}
