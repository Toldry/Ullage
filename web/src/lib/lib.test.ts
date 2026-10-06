import { describe, expect, it } from 'vitest';
import { formatBytes, formatDuration, estimateScanMinutes } from './format';
import { squarify } from './treemap';

describe('formatBytes', () => {
  it('uses 1024-based units', () => {
    expect(formatBytes(0)).toBe('0 B');
    expect(formatBytes(512)).toBe('512 B');
    expect(formatBytes(1536)).toBe('1.5 KB');
    expect(formatBytes(448.9 * 1024 ** 3)).toBe('448.9 GB');
  });
});

describe('formatDuration', () => {
  it('formats minutes and hours', () => {
    expect(formatDuration(108)).toBe('1:48');
    expect(formatDuration(3725)).toBe('1:02:05');
  });
});

describe('estimateScanMinutes', () => {
  it('never says zero', () => {
    expect(estimateScanMinutes(10)).toBe(1);
  });
});

describe('squarify', () => {
  it('fills the bounds exactly with proportional areas', () => {
    const items = [{ value: 6 }, { value: 6 }, { value: 4 }, { value: 3 }, { value: 2 },
      { value: 2 }, { value: 1 }];
    const placed = squarify(items, { x: 0, y: 0, width: 600, height: 400 });
    expect(placed).toHaveLength(items.length);
    const area = placed.reduce((sum, cell) => sum + cell.width * cell.height, 0);
    expect(area).toBeCloseTo(600 * 400, 3);
    for (const cell of placed) {
      expect(cell.width * cell.height).toBeCloseTo((cell.value / 24) * 240000, 3);
      expect(cell.x + cell.width).toBeLessThanOrEqual(600.0001);
      expect(cell.y + cell.height).toBeLessThanOrEqual(400.0001);
    }
  });

  it('returns nothing for empty input', () => {
    expect(squarify([], { x: 0, y: 0, width: 10, height: 10 })).toEqual([]);
  });
});
