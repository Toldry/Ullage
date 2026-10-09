import { useCallback, useEffect, useState } from 'preact/hooks';
import { Checkbox, Kbd } from '../components/Controls';
import { formatBytes, formatDateTime } from '../lib/format';
import { redactionApi, ruleTypeHelp, ruleTypeLabels, treatmentLabels } from '../lib/redaction';
import type { RedactionSummary, RuleDraft, RuleTreatment, RuleType, RuleView } from '../lib/types';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
}

const emptyDraft: RuleDraft = { type: 'name', pattern: '', treatment: 'subtree',
  keepWellKnown: true };

export function RulesScreen({ guard }: Props) {
  const [rules, setRules] = useState<RuleView[] | null>(null);
  const [summary, setSummary] = useState<RedactionSummary | null>(null);
  const [editing, setEditing] = useState<string | null>(null);
  const [draft, setDraft] = useState<RuleDraft>(emptyDraft);
  const [expanded, setExpanded] = useState<string | null>(null);
  // Full match lists loaded with "+ N more", by rule ID.
  const [allHits, setAllHits] = useState<Record<string, string[]>>({});
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    const loadedSummary = await guard(redactionApi.summary());
    setSummary(loadedSummary ?? null);
    setAllHits({});
    if (loadedSummary?.available) setRules((await guard(redactionApi.rules())) ?? []);
  }, [guard]);

  useEffect(() => { void load(); }, [load]);

  const startNew = useCallback(() => {
    setEditing('new');
    setDraft(emptyDraft);
    setError('');
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement;
      if (target.tagName === 'INPUT' || target.tagName === 'SELECT') return;
      if ((event.key === 'n' || event.key === 'N') && !event.ctrlKey && !event.metaKey) {
        event.preventDefault();
        startNew();
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [startNew]);

  const save = async () => {
    setError('');
    try {
      await redactionApi.saveRule(draft);
      setEditing(null);
      await load();
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : String(failure));
    }
  };

  const remove = async (rule: RuleView) => {
    await guard(redactionApi.deleteRule(rule.id));
    await load();
  };

  if (summary && !summary.available) {
    return (
      <div class="screen">
        <RulesHeading storePath={summary.storePath} onNew={startNew} disabled />
        <div class="banner banner-bad">
          <div class="banner-text">
            <div class="banner-title">Redaction rules can't be read</div>
            <div class="muted">
              {summary.error} Open Redaction review (3) to move the file aside and start over.
            </div>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div class="screen screen-wide">
      <RulesHeading storePath={summary?.storePath ?? ''} onNew={startNew} />
      <div class="panel">
        <div class="rules-row rules-head">
          <span>Type</span><span>Pattern</span><span>Treatment</span><span>Matches now</span>
          <span>Last matched</span><span>Created</span><span />
        </div>
        {editing === 'new' && (
          <RuleEditor draft={draft} error={error} onChange={setDraft} onSave={save}
            onCancel={() => setEditing(null)} />
        )}
        {(rules ?? []).map((rule) => editing === rule.id ? (
          <RuleEditor key={rule.id} draft={draft} error={error} onChange={setDraft}
            onSave={save} onCancel={() => setEditing(null)} />
        ) : (
          <div key={rule.id}>
            <div class="rules-row" onClick={() =>
              setExpanded(expanded === rule.id ? null : rule.id)}>
              <span class="small">{ruleTypeLabels[rule.type]}</span>
              <span class="mono ellipsis" title={rule.pattern}>{rule.pattern}</span>
              <span class="small muted">{treatmentLabels[rule.treatment]}</span>
              <span class="cell-mono">
                {rule.inScan ? (
                  <>
                    {rule.matches}{' '}
                    <span class="faint">{rule.matches ? formatBytes(rule.matchBytes) : ''}</span>
                  </>
                ) : <span class="faint">no scan yet</span>}
              </span>
              <span class="small faint">
                {rule.lastMatched ? formatDateTime(rule.lastMatched).split(',')[0] : 'never'}
              </span>
              <span class="small faint">{formatDateTime(rule.created).split(',')[0]}</span>
              <span class="rule-actions">
                <button type="button" class="link-button" onClick={(event) => {
                  event.stopPropagation();
                  setEditing(rule.id);
                  setDraft({ id: rule.id, type: rule.type, pattern: rule.pattern,
                    treatment: rule.treatment, keepWellKnown: rule.keepWellKnown });
                  setError('');
                }}>Edit</button>
                <button type="button" class="link-button danger" onClick={(event) => {
                  event.stopPropagation();
                  void remove(rule);
                }}>Delete</button>
              </span>
            </div>
            {expanded === rule.id && (
              <div class="rule-hits">
                {rule.hits.length === 0 ? (
                  <span class="faint">No matches in the current scan.</span>
                ) : (
                  <>
                    <span class="faint">{rule.matches} matches in this scan:</span>
                    {(allHits[rule.id] ?? rule.hits).map((hit) => (
                      <span class="mono" key={hit}>{hit}</span>
                    ))}
                    {rule.matches > (allHits[rule.id] ?? rule.hits).length && (
                      allHits[rule.id] ? (
                        <span class="faint">
                          + {rule.matches - allHits[rule.id].length} more, not listed
                        </span>
                      ) : (
                        <button type="button" class="link-button" onClick={async (event) => {
                          event.stopPropagation();
                          const hits = await guard(redactionApi.ruleHits(rule.id));
                          if (hits) setAllHits((previous) => ({ ...previous, [rule.id]: hits }));
                        }}>
                          + {rule.matches - rule.hits.length} more
                        </button>
                      )
                    )}
                  </>
                )}
              </div>
            )}
          </div>
        ))}
        {rules && rules.length === 0 && editing !== 'new' && (
          <div class="panel-note">
            No rules yet. Rules remember redactions for future scans; create one here or with
            "Remember" in Redaction review.
          </div>
        )}
      </div>
      <div class="rule-types">
        {(Object.keys(ruleTypeLabels) as RuleType[]).map((type) => (
          <div class="option" key={type}>
            <div class="option-text">
              <span class="option-title">{ruleTypeLabels[type]}</span>
              <span class="option-detail">{ruleTypeHelp[type]}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function RulesHeading(props: { storePath: string; onNew: () => void; disabled?: boolean }) {
  return (
    <div class="screen-header">
      <div class="heading">
        <div class="step-label">Library</div>
        <h1>Redaction rules</h1>
        <p class="lead">
          Applied before every export, so matching items arrive pre-checked in redaction
          review. Stored encrypted for your Windows account in{' '}
          <span class="inline-path">{props.storePath}</span>; rules never leave this PC.
        </p>
      </div>
      <button type="button" class="button button-primary button-with-key"
        disabled={props.disabled} onClick={props.onNew}>
        New rule
        <Kbd inverse>N</Kbd>
      </button>
    </div>
  );
}

function RuleEditor(props: {
  draft: RuleDraft;
  error: string;
  onChange: (draft: RuleDraft) => void;
  onSave: () => void;
  onCancel: () => void;
}) {
  const { draft } = props;
  return (
    <div class="rule-editor">
      <div class="rule-editor-row">
        <select class="select" aria-label="Rule type" value={draft.type}
          onChange={(event) => props.onChange({ ...draft,
            type: (event.target as HTMLSelectElement).value as RuleType })}>
          {(Object.keys(ruleTypeLabels) as RuleType[]).map((type) => (
            <option key={type} value={type}>{ruleTypeLabels[type]}</option>
          ))}
        </select>
        <input class="filter-input mono" aria-label="Pattern" value={draft.pattern}
          placeholder={draft.type === 'name' ? '*tax*' : '~/clients/**'}
          onInput={(event) => props.onChange({ ...draft,
            pattern: (event.target as HTMLInputElement).value })}
          onKeyDown={(event) => {
            if (event.key === 'Enter') props.onSave();
            if (event.key === 'Escape') props.onCancel();
          }} />
        <select class="select" aria-label="Treatment" value={draft.treatment}
          onChange={(event) => props.onChange({ ...draft,
            treatment: (event.target as HTMLSelectElement).value as RuleTreatment })}>
          <option value="subtree">{treatmentLabels.subtree}</option>
          <option value="keep-extension">{treatmentLabels['keep-extension']}</option>
        </select>
        <span class="actions">
          <button type="button" class="button button-primary button-small"
            onClick={props.onSave}>Save</button>
          <button type="button" class="button button-small" onClick={props.onCancel}>
            Cancel
          </button>
        </span>
      </div>
      {draft.treatment === 'subtree' && (
        <label class="toggle-label small">
          <Checkbox state={draft.keepWellKnown ? 'on' : 'off'}
            label="Keep well-known folder names inside"
            onToggle={() => props.onChange({ ...draft, keepWellKnown: !draft.keepWellKnown })} />
          Keep well-known folder names inside readable (build, node_modules, cache…)
        </label>
      )}
      <div class="small faint">{ruleTypeHelp[draft.type]}</div>
      {props.error && <div class="small error-text">{props.error}</div>}
    </div>
  );
}
