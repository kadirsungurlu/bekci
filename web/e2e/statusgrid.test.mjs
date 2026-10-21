// Çalıştırma (web/e2e): node --test --experimental-strip-types statusgrid.test.mjs
import { test } from 'node:test';
import assert from 'node:assert/strict';
import { barCount, columnWidth, gridColumnCount, normalizeColumns } from '../src/lib/statusgrid.ts';

test('normalizeColumns', () => {
  for (const [i, w] of [[0, 0], [1, 1], [6, 6], [7, 0], [-1, 0], [2.5, 0], ['3', 0], [null, 0]])
    assert.equal(normalizeColumns(i), w, String(i));
});

test('otomatik: eski davranış (1-3 sütun)', () => {
  assert.equal(gridColumnCount(0, 8, 375), 1);
  assert.equal(gridColumnCount(0, 8, 820), 2);
  assert.equal(gridColumnCount(0, 8, 1600), 3);
  assert.equal(gridColumnCount(0, 1, 1600), 1); // grup sayısından fazla sütun yok
});

test('seçilen sütun: dar ekranda azalır, mobilde 1', () => {
  for (const c of [1, 2, 3, 6]) assert.equal(gridColumnCount(c, 10, 343), 1, `${c} sütun / telefon`);
  assert.equal(gridColumnCount(6, 10, 1604), 6); // 1920 geniş
  assert.equal(gridColumnCount(6, 10, 1240), 4); // 1280 geniş
  assert.equal(gridColumnCount(6, 10, 820), 3); // dar sayfa
  assert.equal(gridColumnCount(3, 10, 1604), 3);
  assert.equal(gridColumnCount(1, 10, 1604), 1);
  assert.equal(gridColumnCount(6, 2, 1604), 2);
  for (let w = 300; w <= 2000; w += 7)
    for (let c = 1; c <= 6; c++) {
      const n = gridColumnCount(c, 10, w);
      assert.ok(n === 1 || columnWidth(w, n) >= 250, `${c}@${w}: ${n} sütun dar`);
    }
});

test('çubuk sayısı kart genişliğine göre', () => {
  assert.equal(barCount('recent', 250), 20);
  assert.equal(barCount('90d', 250), 20);
  assert.equal(barCount('recent', 600), 60);
  assert.equal(barCount('24h', 250), 24);
});
