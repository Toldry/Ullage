import { useCallback, useEffect, useMemo, useState } from 'preact/hooks';
import { Checkbox, Kbd } from '../components/Controls';
import { formatBytes, formatCount } from '../lib/format';
import {
  isRedacted,
  placeholderSegments,
  reasonText,
  redactionApi,
  ruleTypeLabels,
  treatmentText,
} from '../lib/redaction';
import type {
  RedactionChildren,
  RedactionDetail,
  RedactionItem,
  RedactionSummary,
  RuleType,
} from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  onOpenRules: () => void;
}

type Tab = 'all' | 'redacted' | 'rule';

interface Row {
  item?: RedactionItem;
  depth: number;
  label: string;
  bytes: number;
  kind: 'item' | 'more' | 'files';
}

// Items smaller than this are hidden by default, as in the design; they rarely matter
// for disk space and the list stays readable.
const smallItemBytes = 50 * 1024 ** 2;
const rootParent = -1;

export function RedactScreen({ guard, onOpenRules }: Props) {
  const [summary, setSummary] = useState<RedactionSummary | null>(null);
  const [childrenByParent, setChildrenByParent] = useState<Map<number, RedactionChildren>>(
    new Map());
  const [expanded, setExpanded] = useState<Set<number>>(new Set());
  const [selected, setSelected] = useState<number | null>(null);
  const [detail, setDetail] = useState<RedactionDetail | null>(null);
  const [filter, setFilter] = useState('');
  const [tab, setTab] = useState<Tab>('all');
  const [hideSmall, setHideSmall] = useState(true);
  const [rememberType, setRememberType] = useState<RuleType>('exact');
  const [resetArmed, setResetArmed] = useState(false);
  const code = summary?.sessionCode ?? '';

  const loadChildren = useCallback(async (sessionCode: string, parent: number) => {
    const loaded = await guard(redactionApi.children(sessionCode, parent));
    if (loaded) {
      setChildrenByParent((previous) => new Map(previous).set(parent, loaded));
    }
    return loaded;
  }, [guard]);

  const refresh = useCallback(async () => {
    const loadedSummary = await guard(redactionApi.summary());
    if (!loadedSummary) return;
    setSummary(loadedSummary);
    if (!loadedSummary.available || !loadedSummary.sessionCode) return;
    const parents = [rootParent, ...expanded];
    await Promise.all(parents.map((parent) => loadChildren(loadedSummary.sessionCode, parent)));
  }, [guard, expanded, loadChildren]);

  useEffect(() => {
    (async () => {
      const loadedSummary = await guard(redactionApi.summary());
      if (!loadedSummary) return;
      setSummary(loadedSummary);
      if (!loadedSummary.available || !loadedSummary.sessionCode) return;
      // Open the first levels down to where the space is, like the design.
      let parent = rootParent;
      const opened = new Set<number>();
      for (let depth = 0; depth < 3; depth++) {
        const page = await loadChildren(loadedSummary.sessionCode, parent);
        const largest = page?.items[0];
        if (!largest?.hasChildren) break;
        opened.add(largest.id);
        parent = largest.id;
      }
      await loadChildren(loadedSummary.sessionCode, parent);
      setExpanded(opened);
    })();
  }, [guard, loadChildren]);

  useEffect(() => {
    if (selected === null || !code) return;
    void guard(redactionApi.detail(code, selected)).then((loaded) => loaded && setDetail(loaded));
  }, [selected, code, guard]);

  const toggleExpanded = async (item: RedactionItem) => {
    const next = new Set(expanded);
    if (next.has(item.id)) {
      next.delete(item.id);
    } else {
      next.add(item.id);
      if (!childrenByParent.has(item.id)) await loadChildren(code, item.id);
    }
    setExpanded(next);
  };

  const applyTreatment = async (item: RedactionItem, treatment: string,
    keepWellKnown = true) => {
    const updated = await guard(redactionApi.override(code, item.id, treatment, keepWellKnown));
    if (updated) {
      setSelected(item.id);
      setDetail(updated);
      await refresh();
    }
  };

  // The checkbox follows the design: checked means "this name does not leave the PC".
  const toggleRedaction = (item: RedactionItem) => {
    if (item.source === 'session') return applyTreatment(item, 'default');
    if (isRedacted(item)) return applyTreatment(item, 'none');
    return applyTreatment(item, item.kind === 'file' ? 'keep-extension' : 'subtree');
  };

  const remember = async (item: RedactionItem) => {
    const treatment = item.decision === 'keep-extension' ? 'keep-extension' : 'subtree';
    const saved = await guard(redactionApi.saveRule({ type: rememberType, treatment,
      keepWellKnown: item.keepWellKnown, sessionCode: code, fromNode: item.id }));
    if (saved) {
      await refresh();
      const updated = await guard(redactionApi.detail(code, item.id));
      if (updated) setDetail(updated);
    }
  };

  const rows = useMemo(() => {
    const result: Row[] = [];
    const visit = (parent: number, depth: number) => {
      const page = childrenByParent.get(parent);
      if (!page) return;
      for (const item of page.items) {
        if (hideSmall && item.bytes < smallItemBytes) continue;
        result.push({ item, depth, label: item.name, bytes: item.bytes, kind: 'item' });
        if (expanded.has(item.id)) visit(item.id, depth + 1);
      }
      const hiddenSmall = hideSmall
        ? page.items.filter((item) => item.bytes < smallItemBytes) : [];
      const moreCount = page.moreCount + hiddenSmall.length;
      const moreBytes = page.moreBytes + hiddenSmall.reduce((sum, item) => sum + item.bytes, 0);
      if (moreCount > 0) {
        result.push({ depth, label: `${formatCount(moreCount)} smaller items`,
          bytes: moreBytes, kind: 'more' });
      }
      if (page.filesCount > 0) {
        const label = `${formatCount(page.filesCount)} small files (names never sent)`;
        result.push({ depth, label,
          bytes: page.filesBytes, kind: 'files' });
      }
    };
    visit(rootParent, 0);
    const needle = filter.trim().toLowerCase();
    return result.filter((row) => {
      if (!row.item) return tab === 'all' && !needle;
      if (needle && !row.item.path.toLowerCase().includes(needle)) return false;
      if (tab === 'redacted') return isRedacted(row.item);
      if (tab === 'rule') return row.item.source === 'rule';
      return true;
    });
  }, [childrenByParent, expanded, hideSmall, filter, tab]);

  if (!summary) return <div class="screen" />;

  if (!summary.available) {
    return (
      <div class="screen">
        <Heading />
        <div class="banner banner-bad">
          <div class="banner-text">
            <div class="banner-title">Redaction rules can't be read</div>
            <div class="muted">
              {summary.error} The file is <span class="inline-path">{summary.storePath}</span>.
              It may have been copied from another Windows account or PC, or damaged. Ullage
              will not build prompts until this is resolved, so nothing you meant to hide can
              leak.
            </div>
          </div>
          <button type="button" class="button" onClick={async () => {
            if (!resetArmed) {
              setResetArmed(true);
              return;
            }
            const result = await guard(redactionApi.resetRules());
            if (result) window.location.reload();
          }}>
            {resetArmed ? 'Click again: start with no rules' : 'Move it aside and start over'}
          </button>
        </div>
      </div>
    );
  }

  if (!summary.sessionCode) {
    return (
      <div class="screen">
        <Heading />
        <div class="placeholder">Run a scan first. Redaction review lists its Unknown items.</div>
      </div>
    );
  }

  return (
    <div class="screen screen-wide redact">
      <div class="screen-header">
        <Heading />
        <button type="button" class="button button-primary button-with-key" disabled
          title="Prompt export arrives in Phase 3">
          Continue to export
          <Kbd inverse>4</Kbd>
        </button>
      </div>

      <div class="redact-summary">
        <span>
          <span class="mono strong">{formatCount(summary.redacted)}</span> of{' '}
          {formatCount(summary.unknownItems)} redacted
        </span>
        <span class="separator">|</span>
        <span>
          {formatCount(summary.byRules)} by{' '}
          <button type="button" class="link-button" onClick={onOpenRules}>
            {summary.rules} remembered {summary.rules === 1 ? 'rule' : 'rules'}
          </button>
          , {formatCount(summary.bySession)} this session
        </span>
        <span class="separator">|</span>
        <span>
          Always applied: your profile → <span class="mono strong">~</span>, account names,
          machine name and SIDs ({formatCount(summary.alwaysRedacted)} items)
        </span>
      </div>

      <div class="redact-grid">
        <div class="panel">
          <div class="redact-toolbar">
            <input class="filter-input" placeholder="Filter paths" value={filter}
              aria-label="Filter paths"
              onInput={(event) => setFilter((event.target as HTMLInputElement).value)} />
            <div class="segmented">
              {(['all', 'redacted', 'rule'] as Tab[]).map((value) => (
                <button type="button" key={value}
                  class={`segment${tab === value ? ' active' : ''}`}
                  onClick={() => setTab(value)}>
                  {value === 'all' ? 'All' : value === 'redacted' ? 'Redacted' : 'Rule-matched'}
                </button>
              ))}
            </div>
            <div class="spacer" />
            <label class="toggle-label">
              <Checkbox state={hideSmall ? 'on' : 'off'} label="Hide items under 50 MB"
                onToggle={() => setHideSmall(!hideSmall)} />
              Hiding items under 50 MB
            </label>
          </div>
          <div class="redact-row redact-head">
            <span>Path</span>
            <span class="align-right">Size</span>
            <span>Treatment</span>
            <span>Why checked</span>
          </div>
          {rows.map((row, index) => (
            <TreeRow key={row.item?.id ?? `extra-${index}`} row={row}
              selected={row.item?.id === selected}
              expanded={row.item ? expanded.has(row.item.id) : false}
              onSelect={() => row.item && setSelected(row.item.id)}
              onToggleExpanded={() => row.item && toggleExpanded(row.item)}
              onToggleRedaction={() => row.item && toggleRedaction(row.item)} />
          ))}
          {rows.length === 0 && <div class="panel-note">Nothing matches.</div>}
        </div>

        <Inspector detail={detail} rememberType={rememberType}
          onRememberTypeChange={setRememberType}
          onTreatment={(treatment, keepWellKnown) =>
            detail && applyTreatment(detail.item, treatment, keepWellKnown)}
          onRemember={() => detail && remember(detail.item)}
          onOpenRules={onOpenRules} />
      </div>
    </div>
  );
}

function Heading() {
  return (
    <div class="heading">
      <div class="step-label">Step 3 of 7</div>
      <h1>Redaction review</h1>
      <p class="lead">
        These Unknown items will be described to the AI by path, size, file count, dates and
        file types. Check any name you don't want to leave this machine. Redacted items are
        sent without dates or other details.
      </p>
    </div>
  );
}

function TreeRow(props: {
  row: Row;
  selected: boolean;
  expanded: boolean;
  onSelect: () => void;
  onToggleExpanded: () => void;
  onToggleRedaction: () => void;
}) {
  const { row } = props;
  const item = row.item;
  const indent = { paddingLeft: `${row.depth * 18}px` };
  if (!item) {
    return (
      <div class="redact-row redact-extra">
        <span class="redact-name" style={indent}>
          <span class="caret" />
          <span class="checkbox-spacer" />
          <span class="faint">{row.label}</span>
        </span>
        <span class="cell-mono align-right faint">{formatBytes(row.bytes)}</span>
        <span />
        <span />
      </div>
    );
  }
  const locked = item.source === 'always' || item.source === 'ancestor' ||
    item.source === 'same-name' || item.decision === 'home';
  return (
    <div class={`redact-row${props.selected ? ' selected' : ''}`} onClick={props.onSelect}>
      <span class="redact-name" style={indent}>
        <button type="button" class="caret" aria-label={props.expanded ? 'Collapse' : 'Expand'}
          disabled={!item.hasChildren}
          onClick={(event) => { event.stopPropagation(); props.onToggleExpanded(); }}>
          {item.hasChildren ? (props.expanded ? '▾' : '▸') : ''}
        </button>
        <Checkbox state={isRedacted(item) ? 'on' : 'off'} disabled={locked}
          label={`Redact ${item.name}`} onToggle={props.onToggleRedaction} />
        <span class="mono redact-label">
          {item.decision === 'home' ? `${item.name} (~)` : item.name}
          {item.kind === 'folder' ? '/' : ''}
        </span>
      </span>
      <span class="cell-mono align-right muted">{formatBytes(item.bytes)}</span>
      <span class="small muted">{treatmentText(item)}</span>
      <span class="why">
        {item.source === 'rule' && item.rulePattern ? (
          <span class="rule-chip" title={item.rulePattern}>rule {item.rulePattern}</span>
        ) : reasonText(item) ? (
          <span class={`rule-chip${item.source === 'session' ? ' dashed' : ''}`}>
            {reasonText(item)}
          </span>
        ) : null}
      </span>
    </div>
  );
}

function Rendered({ text }: { text: string }) {
  return (
    <>
      {placeholderSegments(text).map((segment, index) => (
        <span key={index} class={segment.placeholder ? 'placeholder-token' : ''}>
          {segment.text}
        </span>
      ))}
    </>
  );
}

function Inspector(props: {
  detail: RedactionDetail | null;
  rememberType: RuleType;
  onRememberTypeChange: (type: RuleType) => void;
  onTreatment: (treatment: string, keepWellKnown: boolean) => void;
  onRemember: () => void;
  onOpenRules: () => void;
}) {
  const detail = props.detail;
  if (!detail) {
    return (
      <div class="panel panel-raised inspector">
        <div class="inspector-section faint">Select an item to see how it is exported.</div>
      </div>
    );
  }
  const item = detail.item;
  const locked = item.source === 'always' || item.source === 'ancestor' ||
    item.source === 'same-name' || item.decision === 'home';
  const current = item.decision === 'subtree' ? 'subtree'
    : item.decision === 'keep-extension' ? 'keep-extension' : 'none';
  const modes = [
    { value: 'subtree', title: item.kind === 'file' ? 'Redact whole name' : 'Redact whole subtree',
      tag: item.kind === 'file' ? '' : 'default',
      description: item.kind === 'file' ? 'report.pdf becomes <R1>'
        : 'Hides this name and every name inside it.' },
    { value: 'keep-extension', title: 'Redact name, keep extension', tag: '',
      description: 'report.pdf becomes <R1>.pdf' },
    { value: 'none', title: "Don't redact", tag: '',
      description: 'Exported exactly as shown above.' },
  ];
  return (
    <div class="panel panel-raised inspector">
      <div class="inspector-section">
        <div class="section-title">Selected</div>
        <div class="mono inspector-name">{item.name}</div>
        <div class="cell-mono faint">
          {formatBytes(item.bytes)} · {formatCount(item.files)} files
        </div>
      </div>
      <div class="inspector-section">
        <div class="inspector-field">
          <span class="faint small">Real path · stays on this PC</span>
          <span class="mono wrap">{item.path}</span>
        </div>
        <div class="inspector-field">
          <span class="faint small">Exported as</span>
          <span class="mono wrap"><Rendered text={item.rendered} /></span>
        </div>
      </div>
      <div class="inspector-section">
        <div class="section-title">Treatment</div>
        {locked ? (
          <div class="muted small">
            {item.decision === 'home' ? 'Your profile folder is always exported as ~.'
              : item.source === 'ancestor' ? 'A folder above this one is redacted with its ' +
                'whole subtree. Change the treatment there.'
                : item.source === 'same-name' ? 'This name contains a name you redacted ' +
                  'elsewhere, so it is hidden too.'
                  : 'This name contains an account name, the machine name or a Windows SID. ' +
                    'It is always redacted.'}
          </div>
        ) : (
          modes.map((mode) => (
            <button type="button" key={mode.value} class="radio-row"
              onClick={() => props.onTreatment(mode.value, item.keepWellKnown)}>
              <span class={`radio${current === mode.value ? ' on' : ''}`} />
              <span class="radio-text">
                <span class="strong-500">
                  {mode.title} <span class="faint small">{mode.tag}</span>
                </span>
                <span class="small muted">{mode.description}</span>
              </span>
            </button>
          ))
        )}
        {!locked && current === 'subtree' && item.kind !== 'file' && (
          <div class="keep-known">
            <Checkbox state={item.keepWellKnown ? 'on' : 'off'}
              label="Keep well-known folder names inside"
              onToggle={() => props.onTreatment('subtree', !item.keepWellKnown)} />
            <span class="radio-text">
              <span class="strong-500">Keep well-known folder names inside</span>
              <span class="small muted">
                <span class="mono">build, node_modules, cache, logs, renders</span> and similar
                stay readable so the AI can recognize them.
              </span>
            </span>
          </div>
        )}
      </div>
      {item.source === 'rule' && item.rulePattern && (
        <div class="inspector-section">
          <div class="section-title">Covered by rule</div>
          <div class="between">
            <span class="mono small">{item.rulePattern}</span>
            <button type="button" class="link-button" onClick={props.onOpenRules}>Edit rule</button>
          </div>
        </div>
      )}
      {item.source === 'session' && (
        <div class="inspector-section">
          <div class="section-title">Remember for future scans</div>
          <div class="remember-row">
            <select class="select" value={props.rememberType} aria-label="Rule type"
              onChange={(event) =>
                props.onRememberTypeChange((event.target as HTMLSelectElement).value as RuleType)}>
              {(['exact', 'subtree', 'name'] as RuleType[]).map((type) => (
                <option key={type} value={type}>{ruleTypeLabels[type]}</option>
              ))}
            </select>
            <button type="button" class="button button-small" onClick={props.onRemember}>
              Remember
            </button>
          </div>
          <div class="small faint">Not remembered: applies to this session only.</div>
        </div>
      )}
      <div class="inspector-section">
        <div class="section-title">In the prompt</div>
        <div class="prompt-preview">
          <div class="preview-line">
            <span class="muted align-right">{formatBytes(item.bytes, 1)}</span>
            <span class="ellipsis"><Rendered text={item.rendered} /></span>
          </div>
          {detail.preview.map((child) => (
            <div class="preview-line" key={child.id}>
              <span class="muted align-right">{formatBytes(child.bytes, 1)}</span>
              <span class="ellipsis"><Rendered text={child.rendered} /></span>
            </div>
          ))}
        </div>
        <div class="small faint">
          Item numbers and line grouping are assigned at export.
        </div>
      </div>
    </div>
  );
}

