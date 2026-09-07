import test from 'node:test';
import assert from 'node:assert/strict';
import core from '../js/core.js';

const { squarify, mapClientToCanvas, zoomScaleOf, hitTest } = core;

function children(sizes) {
  return sizes.map((s, i) => ({ name: 'n' + i, path: 'C:\\n' + i, totalSize: s }));
}

function totalOf(list) {
  return list.reduce((acc, c) => acc + c.totalSize, 0);
}

function overlaps(a, b) {
  const eps = 1e-6;
  return a.x < b.x + b.w - eps && b.x < a.x + a.w - eps && a.y < b.y + b.h - eps && b.y < a.y + a.h - eps;
}

test('squarify preserva a area total do retangulo', () => {
  const items = children([500, 250, 125, 60, 30, 20, 10, 5]);
  const total = totalOf(items);
  const W = 800;
  const H = 600;
  const rects = squarify(items, total, 0, 0, W, H);

  assert.equal(rects.length, items.length);
  const soma = rects.reduce((acc, r) => acc + r.w * r.h, 0);
  assert.ok(Math.abs(soma - W * H) < 1e-6, `area total ${soma} != ${W * H}`);
});

test('squarify preserva a area proporcional de cada item', () => {
  const items = children([1000, 400, 300, 200, 100, 50, 25, 12, 6, 3]);
  const total = totalOf(items);
  const W = 1000;
  const H = 700;
  const rects = squarify(items, total, 0, 0, W, H);

  rects.forEach((r) => {
    const esperado = (r.node.totalSize / total) * W * H;
    assert.ok(Math.abs(r.w * r.h - esperado) < 1e-6, `${r.node.name}: ${r.w * r.h} != ${esperado}`);
  });
});

test('squarify nao sobrepoe retangulos e mantem tudo dentro do quadro', () => {
  const items = children(Array.from({ length: 40 }, (_, i) => 4000 / (i + 1)));
  const total = totalOf(items);
  const W = 960;
  const H = 540;
  const rects = squarify(items, total, 10, 20, W, H);

  for (const r of rects) {
    assert.ok(r.x >= 10 - 1e-6 && r.y >= 20 - 1e-6, 'retangulo fora do quadro (origem)');
    assert.ok(r.x + r.w <= 10 + W + 1e-6, 'retangulo passa da largura');
    assert.ok(r.y + r.h <= 20 + H + 1e-6, 'retangulo passa da altura');
    assert.ok(r.w >= 0 && r.h >= 0, 'dimensao negativa');
  }
  for (let i = 0; i < rects.length; i++) {
    for (let j = i + 1; j < rects.length; j++) {
      assert.ok(!overlaps(rects[i], rects[j]), `sobreposicao entre ${rects[i].node.name} e ${rects[j].node.name}`);
    }
  }
});

test('squarify mantem aspecto <= 4 nos itens grandes', () => {
  const items = children([3000, 2200, 1800, 1500, 1200, 900, 700, 500, 400, 300, 200, 150, 100, 80, 60, 40]);
  const total = totalOf(items);
  const W = 1200;
  const H = 800;
  const area = W * H;
  const rects = squarify(items, total, 0, 0, W, H);

  const grandes = rects.filter((r) => r.w * r.h >= area * 0.01);
  assert.ok(grandes.length >= 8, 'esperava varios itens grandes no conjunto de teste');
  grandes.forEach((r) => {
    const aspecto = Math.max(r.w / r.h, r.h / r.w);
    assert.ok(aspecto <= 4, `${r.node.name}: aspecto ${aspecto.toFixed(2)} > 4`);
  });
});

test('squarify mantem aspecto <= 4 tambem num quadro estreito', () => {
  const items = children([900, 700, 500, 400, 300, 250, 200, 150]);
  const total = totalOf(items);
  const W = 320;
  const H = 900;
  const rects = squarify(items, total, 0, 0, W, H);
  const grandes = rects.filter((r) => r.w * r.h >= W * H * 0.05);
  grandes.forEach((r) => {
    const aspecto = Math.max(r.w / r.h, r.h / r.w);
    assert.ok(aspecto <= 4, `${r.node.name}: aspecto ${aspecto.toFixed(2)} > 4`);
  });
});

test('squarify devolve lista vazia em entradas degeneradas', () => {
  assert.deepEqual(squarify([], 0, 0, 0, 100, 100), []);
  assert.deepEqual(squarify(children([10]), 10, 0, 0, 0, 100), []);
  assert.deepEqual(squarify(children([10]), 10, 0, 0, 100, -5), []);
  assert.deepEqual(squarify(null, 10, 0, 0, 100, 100), []);
});

test('squarify aguenta muitos itens sem custo quadratico perceptivel', () => {
  const items = children(Array.from({ length: 5000 }, (_, i) => 5000 - i));
  const total = totalOf(items);
  const inicio = Date.now();
  const rects = squarify(items, total, 0, 0, 1600, 1000);
  const decorrido = Date.now() - inicio;
  assert.equal(rects.length, items.length);
  assert.ok(decorrido < 2000, `squarify levou ${decorrido} ms para 5000 itens`);
});

// ---------------------------------------------------------------------
// Mapeamento de coordenadas do treemap sob zoom da interface
// ---------------------------------------------------------------------

/** Simula o getBoundingClientRect de um canvas sob zoom da pagina. */
function rectComZoom(layoutLeft, layoutTop, layoutWidth, layoutHeight, zoom) {
  return {
    left: layoutLeft * zoom,
    top: layoutTop * zoom,
    width: layoutWidth * zoom,
    height: layoutHeight * zoom,
  };
}

test('mapClientToCanvas casa clique e desenho com zoom 100%', () => {
  const rect = rectComZoom(100, 50, 800, 600, 1);
  const p = mapClientToCanvas(100 + 400, 50 + 300, rect, 800, 600);
  assert.ok(Math.abs(p.x - 400) < 1e-9);
  assert.ok(Math.abs(p.y - 300) < 1e-9);
});

test('mapClientToCanvas casa clique e desenho com zoom 80%', () => {
  const zoom = 0.8;
  const rect = rectComZoom(100, 50, 800, 600, zoom);
  // Um bloco desenhado em (400, 300) no espaco de layout aparece aqui na tela:
  const clientX = (100 + 400) * zoom;
  const clientY = (50 + 300) * zoom;
  const p = mapClientToCanvas(clientX, clientY, rect, 800, 600);
  assert.ok(Math.abs(p.x - 400) < 1e-6, `x=${p.x}`);
  assert.ok(Math.abs(p.y - 300) < 1e-6, `y=${p.y}`);
});

test('mapClientToCanvas casa clique e desenho com zoom 120%', () => {
  const zoom = 1.2;
  const rect = rectComZoom(100, 50, 800, 600, zoom);
  const clientX = (100 + 640) * zoom;
  const clientY = (50 + 120) * zoom;
  const p = mapClientToCanvas(clientX, clientY, rect, 800, 600);
  assert.ok(Math.abs(p.x - 640) < 1e-6, `x=${p.x}`);
  assert.ok(Math.abs(p.y - 120) < 1e-6, `y=${p.y}`);
});

test('clique acerta o bloco desenhado em 80% e 120% de zoom', () => {
  const items = children([600, 300, 100]);
  const total = totalOf(items);
  const layoutW = 800;
  const layoutH = 600;
  const rects = squarify(items, total, 0, 0, layoutW, layoutH);

  for (const zoom of [0.8, 1.0, 1.2]) {
    const rect = rectComZoom(64, 32, layoutW, layoutH, zoom);
    for (const alvo of rects) {
      const centroX = alvo.x + alvo.w / 2;
      const centroY = alvo.y + alvo.h / 2;
      const clientX = (64 + centroX) * zoom;
      const clientY = (32 + centroY) * zoom;
      const p = mapClientToCanvas(clientX, clientY, rect, layoutW, layoutH);
      const encontrado = hitTest(rects, p.x, p.y);
      assert.ok(encontrado, `zoom ${zoom}: nenhum bloco encontrado`);
      assert.equal(encontrado.node.name, alvo.node.name, `zoom ${zoom}: bloco errado`);
    }
  }
});

test('mapClientToCanvas nao quebra com retangulo degenerado', () => {
  assert.deepEqual(mapClientToCanvas(10, 10, null, 800, 600), { x: 0, y: 0 });
  const p = mapClientToCanvas(10, 20, { left: 0, top: 0, width: 0, height: 0 }, 0, 0);
  assert.ok(Number.isFinite(p.x) && Number.isFinite(p.y));
});

test('zoomScaleOf devolve o fator de escala visual', () => {
  assert.ok(Math.abs(zoomScaleOf({ width: 640 }, 800) - 0.8) < 1e-9);
  assert.ok(Math.abs(zoomScaleOf({ width: 960 }, 800) - 1.2) < 1e-9);
  assert.equal(zoomScaleOf({ width: 0 }, 800), 1);
  assert.equal(zoomScaleOf(null, 800), 1);
});

test('hitTest devolve o bloco mais profundo e null fora do quadro', () => {
  const nodes = [
    { x: 0, y: 0, w: 100, h: 100, node: { name: 'pai' } },
    { x: 10, y: 10, w: 30, h: 30, node: { name: 'filho' } },
  ];
  assert.equal(hitTest(nodes, 20, 20).node.name, 'filho');
  assert.equal(hitTest(nodes, 80, 80).node.name, 'pai');
  assert.equal(hitTest(nodes, 500, 500), null);
  assert.equal(hitTest([], 1, 1), null);
  assert.equal(hitTest(null, 1, 1), null);
});
