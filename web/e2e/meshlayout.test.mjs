// Çalıştırma (web/e2e): node --test --experimental-strip-types meshlayout.test.mjs
// Go (internal/mesh/topology.go) ile aynı koordinatlar: ortak fikstür.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { MARK_CLEAR, buildTopology, layoutPoints, layoutSize, placeMarks } from '../src/lib/meshlayout.ts';

const fixture = JSON.parse(readFileSync(new URL('../../internal/mesh/testdata/layout.json', import.meta.url), 'utf8'));

test('Go fikstürüyle aynı yerleşim (2-12 düğüm)', () => {
  assert.equal(fixture.length, 11);
  for (const c of fixture) {
    const s = layoutSize(c.n);
    assert.equal(s.width, c.width, `n=${c.n} genişlik`);
    assert.equal(s.height, c.height, `n=${c.n} yükseklik`);
    assert.deepEqual(layoutPoints(c.n), c.points, `n=${c.n} noktalar`);
  }
});

test('buildTopology: sıra, tek yön, özet', () => {
  const t = buildTopology([7, 0, 3], [
    { a: 0, b: 3, state: 'up', ab: 'up', ba: 'up' },
    { a: 0, b: 7, state: 'oneway', ab: 'down', ba: 'up' },
    { a: 3, b: 7, state: 'down', ab: 'down', ba: 'down' },
  ]);
  assert.deepEqual(t.nodes.map((n) => n.id), [0, 3, 7]);
  assert.ok(t.nodes[0].main && t.nodes[0].y < t.nodes[1].y);
  assert.equal(t.links[1].state, 'oneway');
  assert.equal(t.links[1].forward, false);
  assert.deepEqual(t.summary, { up: 1, down: 1, oneway: 1, unknown: 0 });
  const u = buildTopology([0, 1], []);
  assert.equal(u.links[0].state, 'unknown');
});

test('durum işareti başka çizginin ve işaretin üstüne düşmez (3-6 düğüm)', () => {
  for (const n of [3, 4, 5, 6]) {
    const pts = layoutPoints(n);
    const segs = [];
    for (let i = 0; i < n; i++)
      for (let j = i + 1; j < n; j++) segs.push({ x1: pts[i][0], y1: pts[i][1], x2: pts[j][0], y2: pts[j][1] });
    const marks = placeMarks(segs);
    assert.equal(marks.length, segs.length);
    marks.forEach(([x, y], i) => {
      const s = segs[i];
      // kendi çizgisinde
      const own = Math.abs((s.x2 - s.x1) * (y - s.y1) - (s.y2 - s.y1) * (x - s.x1)) / Math.hypot(s.x2 - s.x1, s.y2 - s.y1);
      assert.ok(own < 0.01, `n=${n} işaret ${i} kendi çizgisinde değil`);
      marks.forEach(([x2, y2], j) => {
        if (j > i) assert.ok(Math.hypot(x - x2, y - y2) >= 20, `n=${n} işaretler ${i},${j} üst üste`);
      });
    });
    // Kare şemada iki köşegen ortada kesişir: işaret kesişimde olmaz.
    if (n === 4) {
      const c = [(pts[0][0] + pts[2][0]) / 2, (pts[0][1] + pts[2][1]) / 2];
      marks.forEach(([x, y]) => assert.ok(Math.hypot(x - c[0], y - c[1]) >= MARK_CLEAR, 'işaret kesişimde'));
    }
  }
});
