// Çalıştırma (web/e2e): node --test --experimental-strip-types topomodel.test.mjs
// Topoloji sayfasının saf modeli: ilişkiler, gruplar, seçim görünümü, sorun listesi.
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { DEFAULT_BY, buildGraph, fold, groupOptions, groups, problems, relMap, viewChannels, viewGroups } from '../src/lib/topomodel.ts';
import { fakeTopology } from './topotohum.mjs';

const d = fakeTopology({ mon: 60, srv: 12, pro: 8 });
const g = buildGraph(d);
const ui = (o = {}) => ({ sel: null, q: '', sorun: false, by: DEFAULT_BY, ac: {}, tam: {}, ...o });
const pick = (kind) => [...g.nodes.values()].find((n) => n.kind === kind && (g.rel.get(n.id) ?? []).length);

test('her varlık bir düğüm olur', () => {
  assert.equal(g.mon.length, 60);
  assert.equal(g.srv.length, 12);
  assert.equal(g.cha.length, d.channels.length);
  assert.ok(g.pro.length >= 8);
});

test('ilişki iki yönlüdür', () => {
  for (const kind of ['mon', 'srv', 'pro', 'cha']) {
    const n = pick(kind);
    assert.ok(n, kind);
    for (const id of relMap(g, n.id).keys()) assert.ok(relMap(g, id).has(n.id), `${n.id} <-> ${id}`);
  }
});

test('seçimsiz relMap boştur', () => assert.equal(relMap(g, null).size, 0));

test('grup toplamları düğüm sayısını korur', () => {
  for (const col of ['mon', 'srv', 'pro']) {
    const items = groupOptions(d)[col].flatMap((by) => groups(g, col, by).reduce((a, x) => a + x.items.length, 0));
    for (const c of items) assert.equal(c, g[col].length);
  }
});

test('başka sütundan seçimde yalnız ilişkililer görünür', () => {
  const n = pick('mon');
  const rel = relMap(g, n.id);
  const view = viewGroups(g, 'pro', ui({ sel: n.id }), rel);
  for (const v of view) for (const r of v.rows) assert.ok(rel.has(r.id));
  const shown = view.reduce((a, v) => a + v.rows.length, 0);
  const expected = g.pro.filter((p) => rel.has(p.id)).length;
  assert.equal(shown, expected);
  assert.equal(view.reduce((a, v) => a + v.rest + v.rows.length + (v.dim ? v.size : 0), 0), g.pro.length);
});

test('tam listede grubun hepsi görünür', () => {
  const n = pick('mon');
  const rel = relMap(g, n.id);
  const first = viewGroups(g, 'pro', ui({ sel: n.id }), rel).find((v) => v.rest);
  if (!first) return;
  const v = viewGroups(g, 'pro', ui({ sel: n.id, tam: { [first.id]: true } }), rel).find((x) => x.id === first.id);
  assert.equal(v.rest, 0);
  assert.equal(v.rows.length, v.size);
});

test('arama Türkçe harflerde büyük küçük ayrımı yapmaz, ilişkili satır süzülmez', () => {
  assert.equal(fold('İSTANBUL'), fold('istanbul'));
  const q = fold(g.mon[3].name);
  const view = viewGroups(g, 'mon', ui({ q }), new Map());
  assert.ok(view.flatMap((v) => v.rows).some((r) => r.id === g.mon[3].id));
  const c = g.cha[0];
  const rel = new Map([[c.id, 'ok']]);
  assert.ok(viewChannels(g, ui({ q: 'bulunmayan-metin' }), rel).rows.some((r) => r.id === c.id));
});

test('yalnız sorunlular sağlıklı satırı çıkarır', () => {
  const rows = viewGroups(g, 'mon', ui({ sorun: true }), new Map()).flatMap((v) => v.rows);
  assert.ok(rows.length && rows.length < g.mon.length);
  assert.ok(rows.every((r) => r.st === 'bad' || r.st === 'warn'));
});

test('sorun çizelgesi en ağır durumdan başlar', () => {
  const list = problems(g);
  assert.ok(list.length);
  const tones = list.map((p) => p.tone);
  assert.equal(tones.lastIndexOf('bad'), tones.filter((x) => x === 'bad').length - 1);
});
