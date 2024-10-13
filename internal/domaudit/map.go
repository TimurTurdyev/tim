package domaudit

import (
	"encoding/json"
	"os"
	"strings"
)

// mapPage: страница карты. Граф рисуется своим кодом, внешних библиотек и обращений в сеть нет.
const mapPage = `<!doctype html>
<meta charset="utf-8">
<title>tim domain __DOMAIN__</title>
<style>
 :root { color-scheme: dark; }
 body { margin:0; background:#12151c; color:#dfe4ec; font:13px ui-monospace,SFMono-Regular,Menlo,monospace; }
 header { padding:10px 16px; border-bottom:1px solid #222733; display:flex; gap:16px; align-items:baseline; }
 header b { color:#3fd08a; font-size:15px; }
 header span { color:#7b8496; }
 #wrap { position:absolute; inset:47px 0 0 0; overflow:hidden; cursor:grab; }
 #wrap.drag { cursor:grabbing; }
 svg { width:100%; height:100%; }
 .edge { stroke:#2c3444; stroke-width:1.2; fill:none; }
 .edge.on { stroke:#3fd08a; stroke-width:2; }
 .node rect { fill:#1b2029; stroke:#2c3444; rx:5; }
 .node.on rect { stroke:#3fd08a; }
 .node text { fill:#dfe4ec; dominant-baseline:middle; }
 .node .sub { fill:#7b8496; font-size:11px; }
 .node.ip rect { fill:#17222b; }
 .node.cdn rect { stroke:#c9a227; }
 .node.dead text { fill:#7b8496; }
 .node { cursor:default; }
 .hint { position:absolute; right:14px; bottom:12px; color:#5c6578; }
</style>
<header><b>__DOMAIN__</b><span id="counts"></span></header>
<div id="wrap"><svg id="svg"></svg></div>
<div class="hint">колесо: масштаб, перетаскивание: сдвиг, наведение на узел: связи</div>
<script>
const data = __DATA__;
const svg = document.getElementById('svg'), wrap = document.getElementById('wrap');
const NS = 'http://www.w3.org/2000/svg';
const el = (n, a) => { const e = document.createElementNS(NS, n); for (const k in a) e.setAttribute(k, a[k]); return e; };
const root = el('g', {});
svg.appendChild(root);

const COLW = 320, ROWH = 50, PAD = 30;
const nodes = [], edges = [];

const CHAR = 7.1;
function add(col, row, label, sub, cls) {
  const w = col === 0 ? 220 : 270;
  const n = { x: PAD + col * COLW, y: PAD + row * ROWH, w, h: 26, label, sub, cls: cls || '', id: nodes.length };
  nodes.push(n);
  return n;
}

const addrRows = [];
let row = 0;
for (const a of data.addrs) {
  const span = Math.max(1, a.hosts.length);
  const sub = [a.as && a.as.name, a.as && a.as.country, a.ptr].filter(Boolean).join(' . ');
  const n = add(1, row + (span - 1) / 2, a.ip, sub, a.cdn ? 'ip cdn' : 'ip');
  addrRows.push({ node: n, hosts: a.hosts });
  row += span;
}
const total = Math.max(row, 1);
const apex = add(0, Math.max(0, (total - 1) / 2), data.domain, data.addrs.length + ' адресов', 'apex');

const hostByName = {};
for (const h of data.hosts) hostByName[h.name] = h;
let hrow = 0;
for (const a of addrRows) {
  edges.push([apex, a.node]);
  for (const name of a.hosts) {
    const h = hostByName[name] || {};
    const sub = h.status ? h.status + ' ' + (h.server || '') + ' ' + (h.title || '') : 'не отвечает по HTTP';
    const n = add(2, hrow++, name, sub.trim(), h.status ? '' : 'dead');
    edges.push([a.node, n]);
  }
  if (!a.hosts.length) hrow++;
}

const gE = el('g', {}), gN = el('g', {});
root.appendChild(gE); root.appendChild(gN);
const edgeEls = edges.map(([a, b]) => {
  const x1 = a.x + a.w, y1 = a.y + a.h / 2, x2 = b.x, y2 = b.y + b.h / 2, mid = (x1 + x2) / 2;
  const p = el('path', { class: 'edge', d: 'M' + x1 + ',' + y1 + ' C' + mid + ',' + y1 + ' ' + mid + ',' + y2 + ' ' + x2 + ',' + y2 });
  gE.appendChild(p);
  return { p, a, b };
});
for (const n of nodes) {
  const g = el('g', { class: 'node ' + n.cls });
  g.appendChild(el('rect', { x: n.x, y: n.y, width: n.w, height: n.h }));
  const t = el('text', { x: n.x + 9, y: n.y + n.h / 2 });
  t.textContent = n.label;
  g.appendChild(t);
  if (n.sub) {
    const s = el('text', { x: n.x + 9, y: n.y + n.h + 9, class: 'sub' });
    const lim = Math.floor((n.w - 18) / CHAR);
    s.textContent = n.sub.length > lim ? n.sub.slice(0, lim - 3) + '...' : n.sub;
    s.setAttribute('title', n.sub);
    g.appendChild(s);
  }
  g.addEventListener('mouseenter', () => light(n));
  g.addEventListener('mouseleave', () => light(null));
  gN.appendChild(g);
  n.g = g;
}
function light(n) {
  // Сначала собираем все узлы цепочки, иначе следующая связь гасит то, что зажгла предыдущая.
  const on = new Set();
  for (const e of edgeEls) {
    const hit = n && (e.a === n || e.b === n);
    e.p.classList.toggle('on', !!hit);
    if (hit) { on.add(e.a); on.add(e.b); }
  }
  for (const x of nodes) x.g.classList.toggle('on', on.has(x));
}
document.getElementById('counts').textContent =
  data.hosts.length + ' имён . ' + data.addrs.length + ' адресов';

let k = 1, tx = 0, ty = 0;
const apply = () => root.setAttribute('transform', 'translate(' + tx + ',' + ty + ') scale(' + k + ')');
wrap.addEventListener('wheel', e => {
  e.preventDefault();
  const f = e.deltaY < 0 ? 1.1 : 1 / 1.1, r = wrap.getBoundingClientRect();
  const mx = e.clientX - r.left, my = e.clientY - r.top;
  tx = mx - (mx - tx) * f; ty = my - (my - ty) * f; k *= f;
  apply();
}, { passive: false });
let drag = null;
wrap.addEventListener('mousedown', e => { drag = { x: e.clientX - tx, y: e.clientY - ty }; wrap.classList.add('drag'); });
addEventListener('mousemove', e => { if (drag) { tx = e.clientX - drag.x; ty = e.clientY - drag.y; apply(); } });
addEventListener('mouseup', () => { drag = null; wrap.classList.remove('drag'); });
// Вписываем граф в окно и ставим по центру по вертикали.
const hostRows = Math.max(hrow, 1);
const h = PAD * 2 + Math.max(total, hostRows) * ROWH;
const w = PAD * 2 + 2 * COLW + 270;
k = Math.min(1, wrap.clientWidth / w, wrap.clientHeight / h);
if (k < 1) k = Math.max(0.3, k);
ty = Math.max(0, (wrap.clientHeight - h * k) / 2);
apply();
</script>
`

// WriteMap собирает самодостаточную HTML-страницу с картой.
func WriteMap(rep *Report, path string) error {
	data, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	page := strings.ReplaceAll(mapPage, "__DOMAIN__", rep.Domain)
	page = strings.Replace(page, "__DATA__", string(data), 1)
	return os.WriteFile(path, []byte(page), 0o644)
}
