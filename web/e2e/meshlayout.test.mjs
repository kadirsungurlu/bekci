// Çalıştırma (web/e2e): node --test --experimental-strip-types meshlayout.test.mjs
// Go (internal/mesh/topology.go) ile aynı koordinatlar: ortak fikstür.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { buildTopology, layoutPoints, layoutSize } from '../src/lib/meshlayout.ts';

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
