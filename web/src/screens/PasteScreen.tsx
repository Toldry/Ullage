import { useEffect, useRef, useState } from 'preact/hooks';
import { Kbd } from '../components/Controls';
import { copyText } from '../lib/clipboard';
import { checkLabels, exportApi } from '../lib/exporting';
import { roundsOf, roundTitle, type Round } from '../lib/results';
import { formatCount, formatDateTime } from '../lib/format';
import type { CheckName, ExportView, PasteResult } from '../lib/types';
import { BatchStepper } from './ExportScreen';

interface Props {
  guard: <T>(work: Promise<T>) => Promise<T | undefined>;
  batch: number;
  onBatchChange: (batch: number) => void;
  onGoExport: () => void;
  onGoResults: () => void;
  // Unparsed text per step, kept by the app so leaving step 5 does not lose it.
  drafts: Record<number, string>;
  onDraftChange: (round: number, text: string) => void;
}

const checkOrder: CheckName[] = ['session', 'complete', 'format', 'ids', 'duplicates'];

export function PasteScreen({ guard, batch, onBatchChange, onGoExport, onGoResults, drafts,
  onDraftChange }: Props) {
  const [view, setView] = useState<ExportView | null>(null);
  const [outcome, setOutcome] = useState<PasteResult | null>(null);
  // An imported step shows its reply for review until the user chooses to replace it.
  const [replacing, setReplacing] = useState(false);
  const text = drafts[batch] ?? '';
  const setText = (next: string) => onDraftChange(batch, next);
  const [copyNote, setCopyNote] = useState('');
  const repairElement = useRef<HTMLPreElement>(null);

  useEffect(() => {
    void guard(exportApi.get()).then((loaded) => loaded && setView(loaded));
  }, [guard]);

  const parse = async () => {
    if (!text.trim()) return;
    const result = await guard(exportApi.paste(batch, text));
    if (!result) return;
    setOutcome(result);
    setView(result.export);
    setCopyNote('');
    if (result.imported) {
      setText('');
      setReplacing(false);
    }
  };

  const clear = () => {
    setText('');
    setOutcome(null);
  };

  if (!view) {
    return (
      <div class="screen">
        <PasteHeading code="" />
        <div class="placeholder">Export a prompt first (step 4).</div>
      </div>
    );
  }
  const rounds = roundsOf(view);
  const current = rounds[Math.min(batch, rounds.length) - 1];
  const explaining = current.kind === 'explain';
  const nextBatch = view.batches.find((item) => !item.imported && item.number !== batch);
  const reviewing = current.imported && !replacing;
  const replyName = explaining ? 'the explanations' : `batch ${current.number}`;
  const parsed = outcome?.explanations ?? outcome?.result;

  return (
    <div class="screen screen-wide">
      <div class="screen-header">
        <PasteHeading code={view.code} />
        <BatchStepper count={rounds.length} current={current.number}
          title={roundTitle(current, view.batches.length)}
          done={(number) => rounds[number - 1].imported}
          onPick={(number) => {
            onBatchChange(number);
            setOutcome(null);
            setReplacing(false);
          }} />
      </div>
      <div class="export-grid">
        <div class="paste-column">
          {reviewing && (
            <ImportedReply round={current} onReplace={() => {
              setReplacing(true);
              setOutcome(null);
            }} onGoResults={onGoResults} />
          )}
          {!reviewing && (<>
          <div class="panel">
            <div class="panel-header export-header">
              <span>
                {current.imported ? 'New response (replaces the imported one)' : 'Response'}
              </span>
              <span class="cell-mono faint normal-case">
                {formatCount(text.length)} characters
              </span>
              <span class="spacer" />
              <span class="faint normal-case">Prose and ``` fences around the block are fine</span>
            </div>
            <textarea class="paste-input" spellcheck={false} value={text}
              aria-label={`Reply for ${replyName}`}
              placeholder={`Paste the assistant's reply for ${replyName} here.`}
              onInput={(event) => setText((event.target as HTMLTextAreaElement).value)}
              onKeyDown={(event) => {
                if (event.key === 'Enter' && (event.ctrlKey || event.metaKey)) {
                  event.preventDefault();
                  void parse();
                }
              }} />
          </div>
          <div class="start-row">
            <button type="button" class="button button-primary button-with-key"
              disabled={!text.trim()} onClick={parse}>
              Parse response
              <Kbd inverse>Ctrl Enter</Kbd>
            </button>
            <button type="button" class="button" onClick={clear}>Clear</button>
            {current.imported && (
              <button type="button" class="ghost-button" onClick={() => {
                setReplacing(false);
                setOutcome(null);
              }}>Back to the imported reply</button>
            )}
            <span class="spacer" />
            <span class="small faint">Pasted text never leaves this tab and this PC.</span>
          </div>
          </>)}
          {outcome && (
            <Outcome outcome={outcome} batchNumber={current.number} explaining={explaining}
              batchCount={view.batches.length} nextBatch={nextBatch?.number}
              copyNote={copyNote} repairElement={repairElement}
              onCopyRepair={async () => {
                const result = await copyText(outcome.repair ?? '', repairElement.current);
                setCopyNote(result === 'copied' ? 'Repair prompt copied.'
                  : 'The text is selected: press Ctrl+C.');
              }}
              onNextBatch={(number) => { onBatchChange(number); clear(); onGoExport(); }}
              onCopyPromptAgain={onGoExport} onGoResults={onGoResults} />
          )}
        </div>
        <div class="side-column">
          <div class="panel checks">
            <div class="section-title">Checks</div>
            {checkOrder.map((check) => {
              // An imported reply passed every check; that is the only way it was imported.
              const state = reviewing && !outcome ? 'pass' : !parsed ? 'skip'
                : parsed.problems.some((problem) => problem.check === check) ? 'fail'
                  : check !== 'session' && check !== 'complete' && !parsed.found
                    ? 'skip' : 'pass';
              return (
                <div class="check-row" key={check}>
                  <span class={`dot check-${state}`} />
                  <span class={`check-label check-${state}`}>
                    {state === 'skip' ? '–' : state}
                  </span>
                  <span>
                    {explaining && check === 'ids' ? 'Category numbers' : checkLabels[check]}
                  </span>
                </div>
              );
            })}
          </div>
          <div class="panel">
            {rounds.map((item) => (
              <button type="button" key={item.number}
                class={`batch-row${item.number === current.number ? ' active' : ''}`}
                onClick={() => {
                  onBatchChange(item.number);
                  setOutcome(null);
                  setReplacing(false);
                }}>
                <span>
                  {item.kind === 'explain' ? `Round ${item.number}` : `Batch ${item.number}`}
                </span>
                <span class={`small ${item.imported ? 'ok-text' : 'faint'}`}>
                  {!item.imported ? 'not imported'
                    : item.kind === 'explain' ? `${item.categoryCount} explained`
                      : `${item.categoryCount} categories · ` +
                        `${formatCount(item.classifiedItems)} of ${formatCount(item.items)} items`}
                </span>
              </button>
            ))}
          </div>
          <div class="small muted">
            Imported categories map back to real paths locally. The assistant only ever saw
            redacted names.
          </div>
        </div>
      </div>
    </div>
  );
}

// ImportedReply shows a step's reply exactly as it was pasted, for review. A batch can be
// replaced with a new reply; an explanation round stays as imported, because categories
// it left out are simply queued again.
function ImportedReply(props: { round: Round; onReplace: () => void; onGoResults: () => void }) {
  const { round } = props;
  return (
    <div class="panel imported-reply">
      <div class="panel-header export-header">
        <span>Imported reply</span>
        <span class="cell-mono faint normal-case">
          {round.importedAt ? formatDateTime(round.importedAt) : ''} ·{' '}
          {round.kind === 'explain' ? `${round.categoryCount} explained`
            : `${round.categoryCount} categories`}
          {round.stillQueued ? ` · ${round.stillQueued} left queued` : ''}
        </span>
        <span class="spacer" />
        <span class="faint normal-case">read-only · kept until Ullage closes</span>
      </div>
      <pre class="reply-text">{round.reply || '(not available)'}</pre>
      <div class="start-row reply-actions">
        <button type="button" class="button button-primary" onClick={props.onGoResults}>
          See results
        </button>
        {round.kind === 'batch' ? (
          <button type="button" class="button" onClick={props.onReplace}>
            Paste a new reply
          </button>
        ) : (
          <span class="small faint">
            To explain categories this round left out, request them again on the Results
            screen; they go into a new round.
          </span>
        )}
      </div>
    </div>
  );
}

function PasteHeading({ code }: { code: string }) {
  return (
    <div class="heading">
      <div class="step-label">Step 5 of 7</div>
      <h1>Paste response</h1>
      <p class="lead">
        Paste the assistant's full reply. Ullage parses it in this tab and checks it
        {code ? <> against session <span class="mono strong">{code}</span></> : ''} before
        importing anything.
      </p>
    </div>
  );
}

function Outcome(props: {
  outcome: PasteResult;
  batchNumber: number;
  batchCount: number;
  nextBatch?: number;
  copyNote: string;
  repairElement: { current: HTMLPreElement | null };
  onCopyRepair: () => void;
  onNextBatch: (number: number) => void;
  onCopyPromptAgain: () => void;
  onGoResults: () => void;
  explaining: boolean;
}) {
  const { outcome } = props;
  const parsed = outcome.explanations ?? outcome.result;
  const problems = parsed.problems;
  const sessionMismatch = problems.some((problem) => problem.check === 'session');
  const truncated = parsed.truncated && problems.length === 1;
  const promptName = props.explaining ? 'explanation' : `batch ${props.batchNumber}`;
  if (outcome.valid && outcome.explanations) {
    const { explanations, missing } = outcome.explanations;
    return (
      <div class="result-card tone-ok">
        <div class="result-title">
          Imported {explanations.length}{' '}
          {explanations.length === 1 ? 'explanation' : 'explanations'}
        </div>
        <div class="muted">
          {missing.length
            ? `${missing.length} requested ${missing.length === 1 ? 'category was' :
              'categories were'} not explained and ${missing.length === 1 ? 'stays' : 'stay'} ` +
              'queued for the next round.'
            : 'Every requested category is explained.'}
        </div>
        <div class="actions">
          <button type="button" class="button button-primary" onClick={props.onGoResults}>
            Back to results
          </button>
        </div>
      </div>
    );
  }
  if (outcome.valid) {
    const empty = outcome.result.categories.length === 0;
    return (
      <div class="result-card tone-ok">
        <div class="result-title">
          {empty ? 'Imported: the assistant recognized nothing in this batch'
            : `Imported ${outcome.result.categories.length} categories`}
        </div>
        <div class="muted">
          {formatCount(outcome.result.classified)} items classified. Items the assistant
          left out stay Unknown.
        </div>
        <div class="actions">
          {props.nextBatch ? (
            <button type="button" class="button button-primary"
              onClick={() => props.onNextBatch(props.nextBatch!)}>
              Copy batch {props.nextBatch} prompt
            </button>
          ) : (
            <button type="button" class="button button-primary" onClick={props.onGoResults}>
              See results
            </button>
          )}
        </div>
      </div>
    );
  }
  const tone = truncated ? 'tone-warn' : 'tone-bad';
  const title = sessionMismatch ? 'Session code mismatch'
    : truncated ? 'Response is truncated'
      : `${problems.length} ${problems.length === 1 ? 'problem' : 'problems'} in the response`;
  const body = sessionMismatch
    ? 'This reply belongs to another prompt, probably from an older conversation. Paste ' +
      `the reply to the current ${promptName} prompt, or copy the prompt again.`
    : 'Nothing was imported. The repair prompt lists each problem and asks for the full ' +
      'corrected reply; paste it into the same conversation.';
  return (
    <div class={`result-card ${tone}`}>
      <div class="result-title">{title}</div>
      <div class="muted">{body}</div>
      <div class="problem-list">
        {problems.map((problem, index) => (
          <div class="problem-row" key={index}>
            <span class="cell-mono faint">{problem.line ? `line ${problem.line}` : ''}</span>
            <span class="mono small">{problem.subject}</span>
            <span class="small">{problem.message}</span>
          </div>
        ))}
      </div>
      {outcome.repair && !sessionMismatch && (
        <div class="repair">
          <div class="between">
            <span class="section-title">Repair prompt</span>
            <span class="small faint">paste into the same conversation</span>
          </div>
          <pre class="repair-text" ref={props.repairElement}>{outcome.repair}</pre>
        </div>
      )}
      <div class="actions">
        {sessionMismatch ? (
          <button type="button" class="button button-primary" onClick={props.onCopyPromptAgain}>
            Copy {promptName} prompt again
          </button>
        ) : (
          <button type="button" class="button button-primary" onClick={props.onCopyRepair}>
            Copy repair prompt
          </button>
        )}
        <span class="small faint">{props.copyNote}</span>
      </div>
    </div>
  );
}
