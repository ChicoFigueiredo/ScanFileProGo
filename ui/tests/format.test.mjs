import test from 'node:test';
import assert from 'node:assert/strict';
import core from '../js/core.js';

const { esc, html, formatBytes, formatNumber, formatDate, formatTimeAgo, basename, parentPath, safeParseJSON, renderMarkdown } = core;

test('esc neutraliza todos os metacaracteres de HTML', () => {
  assert.equal(esc('<script>alert(1)</script>'), '&lt;script&gt;alert(1)&lt;/script&gt;');
  assert.equal(esc('a & b'), 'a &amp; b');
  assert.equal(esc('"aspas"'), '&quot;aspas&quot;');
  assert.equal(esc("o'reilly"), 'o&#39;reilly');
  // A crase passa intacta de proposito: atributos sao sempre entre aspas duplas
  // e renderMarkdown precisa reconhecer blocos de codigo depois do escape.
  assert.equal(esc('`crase`'), '`crase`');
});

test('esc protege contexto de atributo', () => {
  const path = 'C:\\tmp\\" onmouseover="steal()';
  const markup = `<div title="${esc(path)}"></div>`;
  assert.ok(!markup.includes('onmouseover="steal()"'));
  assert.ok(markup.includes('&quot;'));
});

test('esc trata nulo, indefinido e números', () => {
  assert.equal(esc(null), '');
  assert.equal(esc(undefined), '');
  assert.equal(esc(0), '0');
  assert.equal(esc(false), 'false');
});

test('html escapa cada valor interpolado', () => {
  const out = html`<span title="${'<b>x</b>'}">${'a&b'}</span>`;
  assert.equal(out, '<span title="&lt;b&gt;x&lt;/b&gt;">a&amp;b</span>');
});

test('formatBytes usa base 1024 e unidades pt-BR', () => {
  assert.equal(formatBytes(0), '0 B');
  assert.equal(formatBytes(null), '0 B');
  assert.equal(formatBytes(512), '512 B');
  assert.equal(formatBytes(1024), '1.00 KB');
  assert.equal(formatBytes(1536), '1.50 KB');
  assert.equal(formatBytes(1024 * 1024), '1.00 MB');
  assert.equal(formatBytes(1024 ** 3), '1.00 GB');
  assert.equal(formatBytes(1024 ** 4), '1.00 TB');
  assert.equal(formatBytes(1024 ** 5), '1.00 PB');
});

test('formatBytes respeita casas decimais e valores negativos', () => {
  assert.equal(formatBytes(1536, 0), '2 KB');
  assert.equal(formatBytes(1536, 3), '1.500 KB');
  assert.equal(formatBytes(-2048), '-2.00 KB');
});

test('formatBytes nao estoura a tabela de unidades', () => {
  const enorme = 1024 ** 8;
  assert.ok(formatBytes(enorme).endsWith(' EB'));
  assert.equal(formatBytes(Infinity), '0 B');
  assert.equal(formatBytes('abc'), '0 B');
});

test('formatNumber devolve 0 para entradas invalidas', () => {
  assert.equal(formatNumber(undefined), '0');
  assert.equal(formatNumber(null), '0');
  assert.equal(formatNumber('xyz'), '0');
  assert.equal(formatNumber(1234), (1234).toLocaleString('pt-BR'));
});

test('formatDate rejeita zero e valores invalidos', () => {
  assert.equal(formatDate(0), '-');
  assert.equal(formatDate(undefined), '-');
  assert.notEqual(formatDate(1700000000), '-');
});

test('formatTimeAgo usa marcos em pt-BR', () => {
  const agora = new Date('2026-09-02T12:00:00Z');
  assert.equal(formatTimeAgo(new Date('2026-09-02T11:59:50Z'), agora), 'há poucos segundos');
  assert.equal(formatTimeAgo(new Date('2026-09-02T11:30:00Z'), agora), 'há 30 min');
  assert.equal(formatTimeAgo(new Date('2026-09-02T06:00:00Z'), agora), 'há 6 horas');
  assert.ok(formatTimeAgo(new Date('2026-08-20T06:00:00Z'), agora).startsWith('em '));
});

test('basename e parentPath entendem caminhos do Windows', () => {
  assert.equal(basename('C:\\Users\\chico\\Projetos'), 'Projetos');
  assert.equal(basename('C:\\Users\\chico\\Projetos\\'), 'Projetos');
  assert.equal(basename('C:\\'), 'C:');
  assert.equal(basename(''), '');
  assert.equal(parentPath('C:\\Users\\chico\\Projetos'), 'C:\\Users\\chico');
  assert.equal(parentPath('C:\\Users'), 'C:\\');
  assert.equal(parentPath(''), '');
});

test('safeParseJSON nunca lanca', () => {
  assert.deepEqual(safeParseJSON('{"a":1}', null), { a: 1 });
  assert.equal(safeParseJSON('{quebrado', 'fallback'), 'fallback');
  assert.equal(safeParseJSON('', 'fallback'), 'fallback');
  assert.equal(safeParseJSON('   ', 'fallback'), 'fallback');
  assert.equal(safeParseJSON(undefined, 'fallback'), 'fallback');
  assert.deepEqual(safeParseJSON('null', []), []);
});

test('renderMarkdown escapa antes de formatar', () => {
  const perigoso = 'Veja **isto**: <img src=x onerror="alert(1)">';
  const out = renderMarkdown(perigoso);
  assert.ok(out.includes('<strong>isto</strong>'));
  assert.ok(!out.includes('<img'));
  assert.ok(out.includes('&lt;img'));
  assert.ok(!out.includes('onerror="alert(1)"'));
});

test('renderMarkdown mantem blocos de codigo escapados', () => {
  const out = renderMarkdown('```go\nfmt.Println("<oi>")\n```');
  assert.ok(out.includes('<pre><code>'));
  assert.ok(out.includes('&lt;oi&gt;'));
  assert.ok(!out.includes('<oi>'));
});

test('renderMarkdown converte quebras de linha', () => {
  assert.equal(renderMarkdown('a\nb'), 'a<br>b');
  assert.equal(renderMarkdown(''), '');
  assert.equal(renderMarkdown(null), '');
});
