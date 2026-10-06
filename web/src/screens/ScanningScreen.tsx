import { Kbd, Swatch } from '../components/Controls';
import { formatBytes, formatClock, formatCount, formatDuration } from '../lib/format';
import type { LogLine, Progress } from '../lib/types';

interface Props {
  progress: Progress;
  log: LogLine[];
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
}

export function ScanningScreen({ progress, log, onPause, onResume, onCancel }: Props) {
  const hasEstimate = progress.expectedBytes > 0;
  // Volume "used" includes metadata the walk never sees, so cap below 100 until done.
  const share = hasEstimate ? Math.min(0.99, progress.allocatedBytes / progress.expectedBytes) : 0;
  const rate = progress.elapsedSeconds > 0
    ? (progress.files + progress.directories) / progress.elapsedSeconds : 0;
  const bytesPerSecond = progress.elapsedSeconds > 0
    ? progress.allocatedBytes / progress.elapsedSeconds : 0;
  const remainingSeconds = hasEstimate && bytesPerSecond > 0
    ? Math.max(0, progress.expectedBytes - progress.allocatedBytes) / bytesPerSecond : 0;
  const title = progress.roots.length ? progress.roots.join(', ') : '…';
  const recentLog = log.slice(-60);

  return (
    <div class="screen">
      <div class="screen-header">
        <div class="heading">
          <div class="step-label">Step 1 of 7</div>
          <h1>Scanning {title}</h1>
          <p class="lead">
            Reading metadata only, at low I/O priority. You can keep working while it runs.
          </p>
        </div>
        <div class="actions">
          {progress.paused ? (
            <button type="button" class="button" onClick={onResume}>Resume</button>
          ) : (
            <button type="button" class="button" onClick={onPause}>Pause</button>
          )}
          <button type="button" class="button button-with-key" onClick={onCancel}>
            Cancel
            <Kbd>Esc</Kbd>
          </button>
        </div>
      </div>

      <div class="panel panel-raised progress-card">
        <div class="progress-headline">
          <span class="progress-percent">
            {hasEstimate ? `${Math.floor(share * 100)}%` : formatCount(progress.files)}
          </span>
          <span class="mono muted">
            {hasEstimate
              ? `${formatBytes(progress.allocatedBytes)} of ` +
                `${formatBytes(progress.expectedBytes)} indexed`
              : `files · ${formatBytes(progress.allocatedBytes)} indexed`}
          </span>
          <span class="spacer" />
          <span class="cell-mono faint">
            {formatDuration(progress.elapsedSeconds)} elapsed
            {hasEstimate && bytesPerSecond > 0 &&
              ` · about ${formatDuration(remainingSeconds)} left`}
            {progress.paused && ' · paused'}
          </span>
        </div>
        <div class="progress-bar" role="progressbar" aria-valuemin={0} aria-valuemax={100}
          aria-valuenow={hasEstimate ? Math.floor(share * 100) : undefined}>
          {hasEstimate ? (
            <div class="progress-bar-fill" style={{ width: `${share * 100}%` }} />
          ) : (
            <div class="progress-bar-indeterminate" />
          )}
        </div>
        <div class="stats">
          <Stat label="Files" value={formatCount(progress.files)} />
          <Stat label="Folders" value={formatCount(progress.directories)} />
          <Stat label="Rate" value={`${formatCount(rate)} entries/s`} />
          <Stat label="Unreadable" value={`${formatCount(progress.deniedCount)} folders`} />
        </div>
        <div class="reading">
          <span class="faint">Reading</span>
          <span class="reading-path">{progress.currentPath || '…'}</span>
        </div>
      </div>

      <div class="two-columns">
        <div class="panel">
          <div class="panel-header">Tiers so far</div>
          <div class="tier-row">
            <Swatch fill="system" />
            <span>System, not analyzed</span>
            <span class="mono">{formatBytes(progress.systemBytes)}</span>
          </div>
          <div class="tier-row">
            <Swatch fill="known" />
            <span>Known · built-in recipes</span>
            <span class="mono">{formatBytes(progress.knownBytes)}</span>
          </div>
          <div class="tier-row">
            <Swatch fill="unknown" />
            <span>Unknown so far</span>
            <span class="mono">{formatBytes(progress.unknownBytes)}</span>
          </div>
        </div>
        <div class="panel">
          <div class="panel-header">Scan log</div>
          <div class="log">
            {recentLog.length === 0 && <span class="faint">Starting…</span>}
            {recentLog.map((line) => (
              <div class="log-line" key={line.sequence}>
                <span class="faint">{formatClock(line.time)}</span>
                <span class={`kind-${line.kind} ${line.kind === 'recipe' ? '' : 'muted'}`}>
                  {line.kind}
                </span>
                <span class="log-text" title={line.path}>
                  {line.text}
                  {line.path ? ` · ${line.path}` : ''}
                </span>
                <span class="align-right">{line.bytes > 0 ? formatBytes(line.bytes) : ''}</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}

function Stat(props: { label: string; value: string }) {
  return (
    <div class="stat">
      <span class="stat-label">{props.label}</span>
      <span class="stat-value">{props.value}</span>
    </div>
  );
}
