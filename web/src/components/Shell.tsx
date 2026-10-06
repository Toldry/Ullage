import type { ComponentChildren } from 'preact';
import { Kbd, Logo } from './Controls';
import { formatBytes, formatCount } from '../lib/format';

export type Screen =
  | 'scan'
  | 'scanning'
  | 'overview'
  | 'redact'
  | 'export'
  | 'paste'
  | 'results'
  | 'delete'
  | 'rules'
  | 'history';

export const steps: { screen: Screen; number: number; label: string }[] = [
  { screen: 'scan', number: 1, label: 'Scan' },
  { screen: 'overview', number: 2, label: 'Overview' },
  { screen: 'redact', number: 3, label: 'Redaction' },
  { screen: 'export', number: 4, label: 'Export prompt' },
  { screen: 'paste', number: 5, label: 'Paste response' },
  { screen: 'results', number: 6, label: 'Results' },
  { screen: 'delete', number: 7, label: 'Delete' },
];

export function TopBar(props: {
  version: string;
  sessionCode: string;
  crumb: string;
  elevated: boolean;
  theme: 'dark' | 'light';
  onToggleTheme: () => void;
}) {
  return (
    <header class="topbar">
      <div class="brand">
        <Logo />
        <span class="brand-name">Ullage</span>
        <span class="brand-version">{props.version}</span>
      </div>
      <div class="crumbs">
        <span class="faint">Session</span>
        <span class="crumb-value">{props.sessionCode || '—'}</span>
        <span class="separator">/</span>
        <span class="crumb-value">{props.crumb}</span>
      </div>
      <div class="spacer" />
      <div
        class="pill"
        title="Ullage listens on 127.0.0.1 only and never makes outbound connections."
      >
        <span class="pill-ring" />
        <span>Offline: no outbound connections</span>
      </div>
      {props.elevated ? (
        <div class="badge">
          <span class="dot dot-ok" />
          Administrator
        </div>
      ) : (
        <div class="badge badge-warn">Not elevated · partial scan</div>
      )}
      <button type="button" class="ghost-button" onClick={props.onToggleTheme}>
        {props.theme === 'dark' ? 'Dark' : 'Light'}
        <Kbd>T</Kbd>
      </button>
    </header>
  );
}

export function Sidebar(props: {
  active: Screen;
  reachedStep: number;
  meta: Partial<Record<Screen, string>>;
  port: number;
  onNavigate: (screen: Screen) => void;
}) {
  const active = props.active === 'scanning' ? 'scan' : props.active;
  return (
    <nav class="sidebar" aria-label="Steps">
      <div class="section-label">Session</div>
      {steps.map((step) => {
        const reached = step.number <= props.reachedStep;
        const isActive = step.screen === active;
        return (
          <button
            type="button"
            key={step.screen}
            class={`nav-item${reached ? ' reached' : ''}${isActive ? ' active' : ''}`}
            disabled={!reached}
            aria-current={isActive ? 'step' : undefined}
            onClick={() => props.onNavigate(step.screen)}
          >
            <span class="nav-number">{step.number}</span>
            <span class="nav-label">{step.label}</span>
            <span class="nav-meta">{reached ? props.meta[step.screen] ?? '' : ''}</span>
          </button>
        );
      })}
      <div class="spacer-small" />
      <div class="section-label">Library</div>
      {[
        { screen: 'rules' as Screen, key: 'R', label: 'Redaction rules' },
        { screen: 'history' as Screen, key: 'H', label: 'History' },
      ].map((item) => (
        <button
          type="button"
          key={item.screen}
          class={`nav-item${props.active === item.screen ? ' active' : ''}`}
          disabled
          title="Arrives in a later phase"
        >
          <span class="nav-number">{item.key}</span>
          <span class="nav-label">{item.label}</span>
        </button>
      ))}
      <div class="spacer" />
      <div class="sidebar-footer">
        <div>127.0.0.1:{props.port}</div>
        <div>token-bound tab · local only</div>
      </div>
    </nav>
  );
}

export function StatusBar(props: {
  volumeSummary?: { name: string; usedBytes: number; totalBytes: number; freeBytes: number };
  files?: number;
  elevated: boolean;
}) {
  const volume = props.volumeSummary;
  const lowSpace = volume !== undefined && volume.freeBytes < volume.totalBytes * 0.1;
  return (
    <footer class="statusbar">
      {volume && (
        <span>
          {volume.name} {formatBytes(volume.usedBytes)} / {formatBytes(volume.totalBytes)} ·{' '}
          <span class={lowSpace ? 'warn-text' : ''}>{formatBytes(volume.freeBytes)} free</span>
        </span>
      )}
      {props.files !== undefined && <span>{formatCount(props.files)} files indexed</span>}
      <span>{props.elevated ? 'elevated' : 'not elevated'}</span>
      <div class="spacer" />
      <span>1–2 steps · Enter start · Esc cancel · T theme</span>
    </footer>
  );
}

export function Main(props: { children: ComponentChildren }) {
  return (
    <main class="main">
      <div class="main-inner">{props.children}</div>
    </main>
  );
}

export function InvalidSession(props: { port: number }) {
  return (
    <div class="centered">
      <div class="invalid-box">
        <div class="brand">
          <Logo />
          <span class="brand-name">Ullage</span>
        </div>
        <div class="heading">
          <h1>This link can't open a session</h1>
          <p class="lead">
            Ullage only accepts the one-time link it opens when you launch it. This one was
            already used, has expired, or was typed or bookmarked by hand.
          </p>
        </div>
        <div class="card">
          <div class="banner-title">Relaunch Ullage from the Start menu.</div>
          <div class="muted">It opens a new tab with a fresh link. You can close this one.</div>
        </div>
        <div class="mono faint">
          <div>127.0.0.1:{props.port || '—'} · session token rejected</div>
          <div>No scan data was loaded into this page.</div>
        </div>
      </div>
    </div>
  );
}
