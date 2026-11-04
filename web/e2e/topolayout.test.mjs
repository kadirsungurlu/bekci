// Çalıştırma (web/e2e): node --test --experimental-strip-types topolayout.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { layoutTopology, overlaps, MIN_CANVAS_W } from '../src/lib/topolayout.ts';

const ids = (n) => Array.from({ length: n }, (_, i) => i + 1);
const g = (key, n, o = {}) => ({ key, ids: ids(n), expanded: false, maxRows: 2, ...o });

function rects(l) {
  const out = [{ ...l.hub, tag: 'hub' }];
  for (const gr of Object.values(l.groups)) {
    for (const c of gr.cards) out.push({ ...c, tag: gr.key + c.id });
    if (gr.more) out.push({ ...gr.more, tag: gr.key + 'more' });
  }
  return out;
}

test('kartlar ve gruplar hiçbir genişlikte üst üste binmez, tuvalden taşmaz', () => {
  for (const w of [640, 800, 1024, 1280, 1440, 1920, 2560])
    for (const [nm, nc, np, ns] of [[0, 0, 0, 0], [4, 2, 4, 4], [60, 9, 10, 30], [200, 1, 25, 0], [1, 0, 1, 1]])
      for (const expanded of [false, true]) {
        const l = layoutTopology(w, {
          monitors: g('monitors', nm, { expanded, maxRows: 3 }),
          channels: nc ? g('channels', nc, { expanded }) : null,
          probes: g('probes', np, { expanded, mesh: true }),
          servers: g('servers', ns, { expanded }),
        });
        const rs = rects(l);
        for (let i = 0; i < rs.length; i++) {
          const r = rs[i];
          assert.ok(r.x >= 0 && r.x + r.w <= l.width + 0.01 && r.y + r.h <= l.height + 0.01, `${w} ${r.tag} taşıyor`);
          for (let j = i + 1; j < rs.length; j++) assert.ok(!overlaps(r, rs[j]), `${w} ${r.tag} x ${rs[j].tag}`);
        }
        const gs = Object.values(l.groups);
        for (let i = 0; i < gs.length; i++)
          for (let j = i + 1; j < gs.length; j++) assert.ok(!overlaps(gs[i].box, gs[j].box), `${w} kutu ${gs[i].key}/${gs[j].key}`);
        for (const gr of gs) for (const c of gr.cards) assert.ok(c.w >= 100, `${w} ${gr.key} kart dar`);
      }
});

test('çok monitörde kapalı grup sınırlı, "daha" kutusu gizli sayıyı taşır', () => {
  const l = layoutTopology(1440, { monitors: g('monitors', 120, { maxRows: 3 }), channels: null, probes: g('probes', 0), servers: g('servers', 0) });
  const m = l.groups.monitors;
  assert.ok(m.more && m.more.hidden + m.cards.length === 120);
  assert.equal(m.perCard, false);
});

test('mesh: 10 noktaya kadar yay, fazlasında matrise düşer', () => {
  const a = layoutTopology(1920, { monitors: g('monitors', 1), channels: null, probes: g('probes', 10, { mesh: true }), servers: g('servers', 2) });
  assert.equal(a.groups.probes.meshArcs, true);
  const b = layoutTopology(1440, { monitors: g('monitors', 1), channels: null, probes: g('probes', 11, { mesh: true }), servers: g('servers', 2) });
  assert.equal(b.groups.probes.meshArcs, false);
  assert.equal(b.groups.probes.meshFallback, true);
  assert.equal(layoutTopology(100, { monitors: g('monitors', 1), channels: null, probes: g('probes', 0), servers: g('servers', 0) }).width, MIN_CANVAS_W);
});

test('mesh yayları sunucuları tek sütuna düşürmez (1440 px tuvali ~1100-1200)', () => {
  for (const w of [1100, 1150, 1200, 1400]) {
    const l = layoutTopology(w, { monitors: g('monitors', 20), channels: g('channels', 3), probes: g('probes', 4, { mesh: true }), servers: g('servers', 6) });
    const cols = new Set(l.groups.servers.cards.map((c) => c.x)).size;
    assert.ok(cols >= 2, `${w}: sunucu sütunu ${cols}`);
    assert.equal(l.groups.probes.meshArcs, true, `${w}: mesh yayları`);
  }
});
