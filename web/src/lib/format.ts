// Sizes use 1024-based units labelled GB/MB, the convention Windows Explorer uses, so the
// numbers match what users see elsewhere on the same PC.
const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB'];

export function formatBytes(bytes: number, digits = 1): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  if (unit === 0) return `${Math.round(value)} B`;
  return `${value.toFixed(value >= 100 ? Math.min(digits, 1) : digits)} ${units[unit]}`;
}

export function formatCount(count: number): string {
  return Math.round(count).toLocaleString('en-US');
}

export function formatDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.round(totalSeconds));
  const hours = Math.floor(seconds / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  const rest = seconds % 60;
  if (hours > 0) return `${hours}:${pad(minutes)}:${pad(rest)}`;
  return `${minutes}:${pad(rest)}`;
}

export function formatLongDuration(totalSeconds: number): string {
  const seconds = Math.max(0, Math.round(totalSeconds));
  const minutes = Math.floor(seconds / 60);
  if (minutes === 0) return `${seconds}s`;
  return `${minutes}m ${pad(seconds % 60)}s`;
}

export function formatClock(isoTime: string): string {
  const date = new Date(isoTime);
  return `${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}`;
}

export function formatDateTime(isoTime: string): string {
  const date = new Date(isoTime);
  const month = date.toLocaleString('en-US', { month: 'short' });
  return `${month} ${date.getDate()}, ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function pad(value: number): string {
  return value.toString().padStart(2, '0');
}

export function pluralize(count: number, noun: string, plural = `${noun}s`): string {
  return `${formatCount(count)} ${count === 1 ? noun : plural}`;
}

// A rough throughput of metadata-only scans on SSDs, used only for the "about N min" hint.
const estimatedBytesPerSecond = 2.5 * 1024 ** 3;

export function estimateScanMinutes(usedBytes: number): number {
  return Math.max(1, Math.round(usedBytes / estimatedBytesPerSecond / 60));
}
