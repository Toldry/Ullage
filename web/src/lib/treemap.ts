// Squarified treemap layout (Bruls, Huizing and van Wijk), ported from the design export.
export interface TreemapInput {
  value: number;
}

export interface Rectangle {
  x: number;
  y: number;
  width: number;
  height: number;
}

export type Placed<T> = T & Rectangle;

interface Sized<T> {
  item: T;
  area: number;
}

export function squarify<T extends TreemapInput>(items: T[], bounds: Rectangle): Placed<T>[] {
  const placed: Placed<T>[] = [];
  const total = items.reduce((sum, item) => sum + item.value, 0);
  if (!total || bounds.width <= 0 || bounds.height <= 0) return placed;
  const scale = (bounds.width * bounds.height) / total;
  const nodes: Sized<T>[] = items.map((item) => ({ item, area: item.value * scale }));
  let remaining = { ...bounds };

  const worstAspect = (row: Sized<T>[], side: number): number => {
    let sum = 0;
    let largest = 0;
    let smallest = Infinity;
    for (const node of row) {
      sum += node.area;
      largest = Math.max(largest, node.area);
      smallest = Math.min(smallest, node.area);
    }
    return Math.max((side * side * largest) / (sum * sum), (sum * sum) / (side * side * smallest));
  };

  const layRow = (row: Sized<T>[]) => {
    const sum = row.reduce((total, node) => total + node.area, 0);
    if (remaining.width >= remaining.height) {
      const columnWidth = sum / remaining.height;
      let y = remaining.y;
      for (const node of row) {
        const height = node.area / columnWidth;
        placed.push({ ...node.item, x: remaining.x, y, width: columnWidth, height });
        y += height;
      }
      remaining = { x: remaining.x + columnWidth, y: remaining.y,
        width: remaining.width - columnWidth, height: remaining.height };
    } else {
      const rowHeight = sum / remaining.width;
      let x = remaining.x;
      for (const node of row) {
        const width = node.area / rowHeight;
        placed.push({ ...node.item, x, y: remaining.y, width, height: rowHeight });
        x += width;
      }
      remaining = { x: remaining.x, y: remaining.y + rowHeight, width: remaining.width,
        height: remaining.height - rowHeight };
    }
  };

  let row: Sized<T>[] = [];
  let index = 0;
  while (index < nodes.length) {
    const side = Math.min(remaining.width, remaining.height);
    const node = nodes[index];
    if (!row.length || worstAspect([...row, node], side) <= worstAspect(row, side)) {
      row.push(node);
      index++;
    } else {
      layRow(row);
      row = [];
    }
  }
  if (row.length) layRow(row);
  return placed;
}
