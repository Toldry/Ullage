import { useCallback, useEffect, useState } from 'preact/hooks';
import { Checkbox, Kbd } from '../components/Controls';
import { copyText } from '../lib/clipboard';
import { formatBytes, formatCount, formatClock, pluralize } from '../lib/format';
import {
  itemKey, restKey, resultsApi, selectedTotals, selectionState,
  toggleCategory, type Selection,
} from '../lib/results';
import type { ResultCategory, ResultItem, ResultsView } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  selection: Selection;
  onSelectionChange: (selection: Set<string>) => void;
  // Called with every new result list, so the owner can drop selections that went away.
  onResultsLoaded: (categories: ResultCategory[]) => void;
  onGoPaste: () => void;
  onRescan: () => void;
  onReviewDeletion: () => void;
  // Changes after a deletion, so the list is loaded again.
  refreshToken: number;
}

type SourceFilter = 'all' | 'recipe' | 'ai';
type ConfidenceFilter = 'any' | 'recipe' | 'high' | 'med' | 'low';
type RegenerableFilter = 'any' | 'yes' | 'partially' | 'no';

export function ResultsScreen({ guard, selection, onSelectionChange, onResultsLoaded,
  onGoPaste, onRescan, onReviewDeletion, refreshToken }: Props) {
  const [view, setView] = useState<ResultsView | null>(null);
  const [expanded, setExpanded] = useState<Set<string>>(new Set());
  const [focused, setFocused] = useState(0);
  const [source, setSource] = useState<SourceFilter>('all');
  const [confidence, setConfidence] = useState<ConfidenceFilter>('any');
  const [regenerable, setRegenerable] = useState<RegenerableFilter>('any');
  const [showKept, setShowKept] = useState(false);

  const accept = useCallback((loaded: ResultsView | undefined) => {
    if (!loaded) return;
    setView(loaded);
    onResultsLoaded(loaded.categories);
  }, [onResultsLoaded]);

  useEffect(() => {
    void guard(resultsApi.get()).then(accept);
  }, [guard, accept, refreshToken]);

  const rows = (view?.categories ?? []).filter((category) =>
    (source === 'all' || category.source === source) &&
    (confidence === 'any' || category.confidence === confidence) &&
    (regenerable === 'any' || category.regenerable === regenerable));
  const keptRows = showKept ? view?.kept ?? [] : [];
  const allRows = [...rows, ...keptRows];

  const toggleExpand = useCallback(async (category: ResultCategory) => {
    const opening = !expanded.has(category.key);
    setExpanded((previous) => {
      const next = new Set(previous);
      if (opening) next.add(category.key);
      else next.delete(category.key);
      return next;
    });
    // Opening the row is what unlocks a low-confidence category, so the server records it.
    if (opening && category.needsExpand) accept(await guard(resultsApi.expand(category.key)));
  }, [expanded, guard, accept]);

  const toggleSelected = useCallback((category: ResultCategory) => {
    if (category.selectable) onSelectionChange(toggleCategory(category, selection));
  }, [selection, onSelectionChange]);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName)) return;
      if (event.ctrlKey || event.metaKey || event.altKey || allRows.length === 0) return;
      const current = allRows[Math.min(focused, allRows.length - 1)];
      if (event.key === 'j') setFocused(Math.min(focused + 1, allRows.length - 1));
      else if (event.key === 'k') setFocused(Math.max(focused - 1, 0));
      else if (event.key === 'Enter' && target.tagName !== 'BUTTON') void toggleExpand(current);
      else if (event.key === 'x') toggleSelected(current);
      else if ((event.key === 'd' || event.key === 'D') && selection.size > 0) {
        onReviewDeletion();
      } else return;
      event.preventDefault();
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

  if (!view) return <div class="screen" />;

  const totals = selectedTotals(view.categories, selection);
  const largest = Math.max(1, ...allRows.map((category) => category.bytes));
  const counts = {
    all: view.categories.length,
    recipe: view.categories.filter((category) => category.source === 'recipe').length,
    ai: view.categories.filter((category) => category.source === 'ai').length,
  };
  const keptBytes = view.kept.reduce((sum, category) => sum + category.bytes, 0);

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <div class="heading">
          <div class="step-label">Step 6 of 6</div>
          <h1>Delete</h1>
          <p class="lead">
            {view.batchCount
              ? `${view.batchesImported} of ${view.batchCount} ` +
                `${view.batchCount === 1 ? 'batch' : 'batches'} parsed. `
              : 'No AI replies imported yet; showing what the recipes found. '}
            Read what each category is, select what to delete, then review. Nothing is
            deleted until you confirm.
          </p>
        </div>
        {view.categories.length > 0 && (
          <div class="results-total">
            <span class="results-total-bytes">{formatBytes(view.summary.reclaimableBytes)}</span>
            <span class="small muted">
              reclaimable across {pluralize(view.categories.length, 'category', 'categories')}
            </span>
          </div>
        )}
      </div>

      {view.batchCount > view.batchesImported && (
        <div class="queued-note">
          <span class="grow">
            {view.batchCount - view.batchesImported} of {view.batchCount}{' '}
            {view.batchCount === 1 ? 'batch has' : 'batches have'} no imported reply yet;
            their items are not listed here.
          </span>
          <button type="button" class="button" onClick={onGoPaste}>Paste a reply</button>
        </div>
      )}

      {view.categories.length === 0 ? (
        <EmptyResults view={view} keptBytes={keptBytes} onRescan={onRescan}
          onShowKept={() => setShowKept(true)} />
      ) : (
        <div class="results-toolbar">
          <div class="segmented" role="group" aria-label="Source">
            {(['all', 'recipe', 'ai'] as const).map((value) => (
              <button type="button" key={value}
                class={`segment${source === value ? ' active' : ''}`}
                aria-pressed={source === value} onClick={() => setSource(value)}>
                {value === 'all' ? 'All' : value === 'recipe' ? 'Recipes' : 'AI'}
                <span class="cell-mono faint">{counts[value]}</span>
              </button>
            ))}
          </div>
          <label class="filter-select">
            <span>Confidence</span>
            <select class="select" value={confidence} onChange={(event) =>
              setConfidence((event.target as HTMLSelectElement).value as ConfidenceFilter)}>
              <option value="any">any</option>
              <option value="recipe">recipe</option>
              <option value="high">high</option>
              <option value="med">medium</option>
              <option value="low">low</option>
            </select>
          </label>
          <label class="filter-select">
            <span>Regenerable</span>
            <select class="select" value={regenerable} onChange={(event) =>
              setRegenerable((event.target as HTMLSelectElement).value as RegenerableFilter)}>
              <option value="any">any</option>
              <option value="yes">yes</option>
              <option value="partially">partially</option>
              <option value="no">no</option>
            </select>
          </label>
          <span class="spacer" />
          <span class="cell-mono muted">
            {totals.items ? `${pluralize(totals.items, 'item')} · ${formatBytes(totals.bytes)} ` +
              'selected' : 'nothing selected'}
          </span>
          <button type="button" class="button button-primary button-with-key"
            disabled={totals.items === 0} onClick={onReviewDeletion}
            title={totals.items === 0 ? 'Select items to delete first' : undefined}>
            Review deletion
            <Kbd inverse>D</Kbd>
          </button>
        </div>
      )}

      {allRows.length > 0 && (
        <div class="results-table" role="table" aria-label="Categories">
          <div class="results-row results-head" role="row">
            <span /><span /><span>Category</span><span class="align-right">Reclaimable</span>
            <span class="align-right">Items</span><span class="results-source">Source</span>
            <span>Confidence</span><span>Regenerable</span><span />
          </div>
          {allRows.map((category, index) => (
            <CategoryRow key={category.key} category={category} largest={largest}
              kept={index >= rows.length}
              focused={index === Math.min(focused, allRows.length - 1)}
              expanded={expanded.has(category.key)}
              selection={selection}
              onFocus={() => setFocused(index)}
              onToggleExpand={() => void toggleExpand(category)}
              onToggleSelected={() => toggleSelected(category)}
              onToggleItem={(key) => {
                const next = new Set(selection);
                if (next.has(key)) next.delete(key);
                else next.add(key);
                onSelectionChange(next);
              }} />
          ))}
        </div>
      )}

      <div class="small muted results-footer">
        Not listed: {formatBytes(view.summary.systemBytes)} System (not analyzed),{' '}
        {formatBytes(view.summary.knownKeptBytes)} Known that recipes keep (installed apps
        and categories under {formatBytes(view.minimumItemBytes)}),{' '}
        {formatBytes(view.summary.unclassifiedBytes)} Unknown not classified
        {view.kept.length > 0 && <>
          , and{' '}
          <button type="button" class="link-button" onClick={() => setShowKept(!showKept)}>
            {formatBytes(keptBytes)} in {pluralize(view.kept.length, 'category',
              'categories')} the AI suggests keeping
          </button>
        </>}. Keys: j / k move, Enter expand, x select, D review deletion.
      </div>
    </div>
  );
}

function EmptyResults(props: {
  view: ResultsView;
  keptBytes: number;
  onRescan: () => void;
  onShowKept: () => void;
}) {
  const { summary } = props.view;
  return (
    <div class="results-empty">
      <div class="result-title">Nothing worth reclaiming</div>
      <div class="muted">
        {props.view.batchesImported
          ? 'The recipes and the AI replies found no category above the ' +
            `${formatBytes(props.view.minimumItemBytes)} threshold that can be deleted.`
          : 'The recipes found nothing above the threshold. Export the prompt (step 4) to ' +
            'let an assistant classify the Unknown items.'}
      </div>
      <div class="results-empty-grid">
        <span>System, not analyzed</span>
        <span class="mono align-right">{formatBytes(summary.systemBytes)}</span>
        <span>Known, kept by recipes (installed apps, small categories)</span>
        <span class="mono align-right">{formatBytes(summary.knownKeptBytes)}</span>
        <span>Unknown, classified by the AI as files to keep</span>
        <span class="mono align-right">{formatBytes(summary.keptByAiBytes)}</span>
        <span>Unknown, not classified</span>
        <span class="mono align-right">{formatBytes(summary.unclassifiedBytes)}</span>
        <span class="muted">Reclaimable</span>
        <span class="mono align-right muted">0 B</span>
      </div>
      <div class="actions">
        <button type="button" class="button" onClick={props.onRescan}>Scan again</button>
        {props.view.kept.length > 0 && (
          <button type="button" class="ghost-button" onClick={props.onShowKept}>
            Browse classified items
          </button>
        )}
      </div>
    </div>
  );
}

const confidenceLabels = { recipe: 'Recipe', high: 'High', med: 'Medium', low: 'Low' };
const confidenceBars = { recipe: 3, high: 3, med: 2, low: 1 };

function CategoryRow(props: {
  category: ResultCategory;
  largest: number;
  kept: boolean;
  focused: boolean;
  expanded: boolean;
  selection: Selection;
  onFocus: () => void;
  onToggleExpand: () => void;
  onToggleSelected: () => void;
  onToggleItem: (key: string) => void;
}) {
  const { category } = props;
  const state = selectionState(category, props.selection);
  const single = category.itemCount === 1 ? category.items[0] : undefined;
  const checkboxTitle = props.kept ? 'The AI suggests keeping these'
    : !category.bruteForce.allowed ? 'Managed cleanup only'
      : category.needsExplanation ? 'The AI reply did not explain this category'
        : category.needsExpand ? 'Expand the row to enable delete' : 'Select for deletion';
  return (
    <>
      <div role="row" tabIndex={-1}
        class={`results-row${props.focused ? ' focused' : ''}${props.expanded ? ' open' : ''}` +
          `${props.kept ? ' kept' : ''}` +
          `${category.explanation && !props.expanded ? ' has-explanation' : ''}`}
        onClick={() => { props.onFocus(); props.onToggleExpand(); }}>
        <span title={checkboxTitle}>
          <Checkbox state={state} label={`Select ${category.name}`}
            disabled={!category.selectable} onToggle={props.onToggleSelected} />
        </span>
        <span class="caret">{props.expanded ? '▾' : '▸'}</span>
        <span class="results-name">
          <span class="strong-500 nowrap">{category.name}</span>
          {category.itemCount > 1 && (
            <span class="cell-mono muted">×{formatCount(category.itemCount)}</span>
          )}
          {single && <span class="cell-mono faint ellipsis">{single.path}</span>}
          {category.restoredFrom && (
            <span class="restored-badge">restored from {category.restoredFrom}</span>
          )}
          {category.cloudSyncItems > 0 && (
            <span class="cloud-badge" title="Deleting also deletes the cloud copy">
              cloud-synced
            </span>
          )}
          {category.bruteForce.warning && (
            <span class="warning-badge" title={category.bruteForce.warning}>
              ⚠ managed cleanup preferred
            </span>
          )}
        </span>
        <span class="results-size">
          <span class="results-bar"
            style={{ width: `${Math.max(2, (category.bytes / props.largest) * 56)}px` }} />
          <span class="cell-mono">{formatBytes(category.bytes)}</span>
        </span>
        <span class="cell-mono muted align-right">{formatCount(category.itemCount)}</span>
        <span class="results-source small muted">
          <span class={`source-swatch source-${category.source}`} />
          {category.source === 'recipe' ? 'Recipe' : 'AI'}
        </span>
        <span class="small muted results-confidence">
          <span class="confidence-bars" aria-hidden="true">
            {[1, 2, 3].map((bar) => (
              <span key={bar} class={`confidence-bar bar-${bar}` +
                `${bar <= confidenceBars[category.confidence] ? ' on' : ''}` +
                ` confidence-${category.confidence}`} />
            ))}
          </span>
          {confidenceLabels[category.confidence]}
        </span>
        <span class="small muted results-regen">
          <span class={`dot regen-${category.regenerable}`} />
          {category.regenerable}
        </span>
        <span class="results-action small">
          <RowAction {...props} />
        </span>
      </div>
      {category.explanation && !props.expanded && (
        <div class={`results-explanation small${props.focused ? ' focused' : ''}`}
          onClick={() => {
          props.onFocus();
          props.onToggleExpand();
        }}>
          <span class="muted">{category.explanation.what}</span>{' '}
          <span><span class="faint">Lost if deleted:</span> {category.explanation.lost}</span>
        </div>
      )}
      {props.expanded && <CategoryDetail {...props} />}
    </>
  );
}

function RowAction(props: { category: ResultCategory; kept: boolean }) {
  const { category } = props;
  if (props.kept) return <span class="faint">AI suggests keeping</span>;
  if (!category.bruteForce.allowed) return <span class="faint">Managed cleanup only</span>;
  if (category.needsExplanation) return <span class="faint">Not explained</span>;
  if (category.needsExpand) return <span class="faint">Expand to enable delete</span>;
  return null;
}

function provenance(category: ResultCategory): string {
  if (category.source === 'recipe') {
    return `Built-in recipe · ${category.recipeId} v${category.recipeVersion}`;
  }
  const confidence = { high: 'high confidence', med: 'medium confidence',
    low: 'low confidence', recipe: '' }[category.confidence];
  const received = category.importedAt ? ` · received ${formatClock(category.importedAt)}` : '';
  return `AI · batch ${category.batch} · ${confidence}${received}`;
}

function CategoryDetail(props: {
  category: ResultCategory;
  kept: boolean;
  selection: Selection;
  onToggleSelected: () => void;
  onToggleItem: (key: string) => void;
}) {
  const { category } = props;
  const explanation = category.explanation;
  const state = selectionState(category, props.selection);
  return (
    <div class="results-detail">
      <div class="detail-column">
        <div class="cell-mono faint">{provenance(category)}</div>
        {explanation ? (
          <>
            <Field label="What it is" text={explanation.what} />
            <Field label="What is lost by deleting" text={explanation.lost} />
          </>
        ) : (
          <div class="explain-box">
            <div class="strong">No explanation</div>
            <div class="small muted">
              The AI grouped these as “{category.name}” but its reply did not describe them,
              so they can't be selected. Paste a corrected reply for batch {category.batch}
              in step 5 to explain them.
            </div>
          </div>
        )}
        <div class="small muted regen-line">
          <span class={`dot regen-${category.regenerable}`} />
          Regenerable: <span class="strong-500">{category.regenerable}</span>
        </div>
        {category.cloudSyncItems > 0 && <CloudWarning category={category} />}
        {category.confidence === 'low' && !props.kept && (
          <div class="low-confidence-note small">
            <span class="strong bad-text">Low confidence.</span>{' '}
            <span class="muted">
              Delete stays off until a row like this has been opened.{' '}
              {category.needsExplanation
                ? 'It also needs an explanation from the AI.'
                : 'It is enabled now; check the items on the right first.'}
            </span>
          </div>
        )}
      </div>

      <div class="detail-column">
        <div class="section-title">Cleanup</div>
        {explanation && explanation.steps.length > 0 && (
          <div class="cleanup-box">
            <div class="between">
              <span class="strong">Managed</span>
              <span class="small faint">
                {category.source === 'recipe' ? 'in the owning tool' : 'advice from the AI'}
              </span>
            </div>
            {explanation.steps.map((step, index) => (
              <div class="managed-step" key={index}>
                <span class="cell-mono faint">{index + 1}</span>
                <div class="managed-step-body">
                  <span class="small">{step.text}</span>
                  {step.command && <CommandLine command={step.command} />}
                </div>
              </div>
            ))}
            {explanation.note && <div class="small muted managed-note">{explanation.note}</div>}
          </div>
        )}
        {!props.kept && category.needsExplanation ? (
          <div class="small faint">
            Without an explanation these items can't be selected.
          </div>
        ) : props.kept ? (
          <div class="cleanup-box dashed">
            <div class="between">
              <span class="strong muted">Brute force</span>
              <span class="cell-mono faint">not offered</span>
            </div>
            <div class="small muted">
              The AI marked these as files most people would want to keep.
            </div>
          </div>
        ) : category.bruteForce.allowed ? (
          <div class="cleanup-box">
            <div class="between">
              <span class="strong">Brute force</span>
              <span class="small faint">to Recycle Bin</span>
            </div>
            <div class="small muted">
              Move the selected items to the Recycle Bin. Nothing outside them is touched.
            </div>
            {category.bruteForce.warning && (
              <div class="brute-warning small" role="note">
                <span class="warn-text strong">⚠ Prefer the managed steps if you can.</span>{' '}
                {category.bruteForce.warning}
              </div>
            )}
            <div>
              <button type="button" class="button button-small"
                disabled={!category.selectable} onClick={props.onToggleSelected}>
                {state === 'on' ? 'Deselect all' : `Select all ${formatCount(
                  category.itemCount)}`}
              </button>
            </div>
          </div>
        ) : (
          <div class="cleanup-box dashed">
            <div class="between">
              <span class="strong muted">Brute force</span>
              <span class="cell-mono faint">not offered</span>
            </div>
            <div class="small muted">{category.bruteForce.reason}</div>
          </div>
        )}
      </div>

      <div class="detail-column">
        <div class="between">
          <span class="section-title nowrap">
            {pluralize(category.itemCount, 'item')} · real names
          </span>
          <span class="cell-mono faint ellipsis">{category.root}</span>
        </div>
        <div class="item-list">
          {category.items.map((item) => (
            <ItemRow key={item.key} item={item} root={category.root}
              disabled={!category.selectable}
              selected={props.selection.has(itemKey(category, item.key))}
              onToggle={() => props.onToggleItem(itemKey(category, item.key))} />
          ))}
          {category.moreItems > 0 && (
            <div class="item-row">
              <Checkbox state={props.selection.has(restKey(category)) ? 'on' : 'off'}
                label="Select the remaining items" disabled={!category.selectable}
                onToggle={() => props.onToggleItem(restKey(category))} />
              <span class="small muted">
                + {formatCount(category.moreItems)} more · {formatBytes(category.moreBytes)}
              </span>
              <span />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

function CloudWarning({ category }: { category: ResultCategory }) {
  const all = category.cloudSyncItems === category.itemCount;
  return (
    <div class="cloud-note small">
      <span class="strong warn-text">Cloud-synced.</span>{' '}
      <span class="muted">
        {all ? 'These items are' : `${pluralize(category.cloudSyncItems, 'item')} of ` +
          `${category.itemCount} ${category.cloudSyncItems === 1 ? 'is' : 'are'}`} in a
        cloud-sync folder such as OneDrive. Deleting them here also deletes them from the
        cloud and your other devices.
        {category.notLocalBytes > 0 && ` ${formatBytes(category.notLocalBytes)} of it is ` +
          'only in the cloud and frees no space on this PC.'}
      </span>
    </div>
  );
}

function Field(props: { label: string; text: string }) {
  return (
    <div class="detail-field">
      <div class="section-title">{props.label}</div>
      <div>{props.text}</div>
    </div>
  );
}

function CommandLine({ command }: { command: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div class="command-line">
      <span class="faint">$</span>
      <span class="command-text">{command}</span>
      <button type="button" class="link-button small" onClick={async () => {
        setCopied((await copyText(command)) === 'copied');
      }}>{copied ? 'copied' : 'copy'}</button>
    </div>
  );
}

function ItemRow(props: {
  item: ResultItem;
  root: string;
  disabled: boolean;
  selected: boolean;
  onToggle: () => void;
}) {
  const { item } = props;
  const relative = props.root && item.path.startsWith(props.root)
    ? item.path.slice(props.root.length) : item.path;
  const details = [
    item.kind === 'loose' ? 'rest of the folder' : null,
    item.files > 1 ? pluralize(item.files, 'file') : null,
    item.modified ? `changed ${formatMonth(item.modified)}` : null,
    item.exportedAs ? `exported as ${item.exportedAs}` : null,
    item.cloudSync ? 'cloud-synced' : null,
  ].filter(Boolean).join(' · ');
  return (
    <div class="item-row">
      <Checkbox state={props.selected ? 'on' : 'off'} label={`Select ${relative}`}
        disabled={props.disabled} onToggle={props.onToggle} />
      <span class="item-text">
        <span class="mono small ellipsis">{relative}</span>
        <span class="small faint ellipsis">{details}</span>
      </span>
      <span class="cell-mono align-right">{formatBytes(item.bytes)}</span>
    </div>
  );
}

function formatMonth(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toLocaleDateString('en-US',
    { month: 'short', year: 'numeric' });
}
