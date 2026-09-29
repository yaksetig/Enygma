const fmt = value => Number(value).toLocaleString("en-US");
const short = (value, n = 12, end = 6) => value ? String(value).length > n + end + 1 ? `${String(value).slice(0, n)}…${String(value).slice(-end)}` : String(value) : "—";
const nameOf = (p, id) => p.registrations.find(r => r.partyId === id)?.name || "You";
export const treePageSize = () => window.innerWidth <= 600 ? 2 : window.innerWidth <= 1050 ? 4 : 8;
const views = new Map();
export function noteTreeState(p, assetId) {
  const key = `${p.id}:${assetId}`;
  if (!views.has(key)) views.set(key, { treeStart: 0, treeFollow: true, leafId: "" });
  return views.get(key);
}

export function commitmentTreeExplorer(p, { assetId = "USD", viewer = "public", showTraffic = false, state, control = "note", depth: fixedDepth, capacity, vault = "Asset vault" }) {
  const tree = p.trees?.[assetId];
  const leaves = p.leaves.filter(leaf => (tree?.leafIds || []).includes(leaf.id));
  const treeDepth = fixedDepth ?? Math.max(0, (tree?.levels.length || 2) - 1);
  const prefix = control === "retail" ? "retail" : "note";
  const elementId = control === "retail" ? "retail" : `note-${p.id}-${assetId}`;
  const trafficOn = p.traffic && (p.id !== "dvp" || p.trafficAsset === assetId);
  const pageSize = treePageSize();
  const count = leaves.length;
  const lastStart = Math.floor(Math.max(0, count - 1) / pageSize) * pageSize;
  const start = state.treeFollow ? lastStart : Math.min(Math.floor(state.treeStart / pageSize) * pageSize, lastStart);
  const visible = leaves.slice(start, start + pageSize);
  const depth = Math.min(treeDepth, Math.log2(pageSize), Math.max(1, Math.ceil(Math.log2(Math.max(2, count)))));
  const slots = 2 ** depth, width = Math.max(280, slots * 136), height = 144 + depth * 90;
  const newest = leaves.at(-1)?.sourceTxId;
  const known = (p.notes || []).filter(n => n.ownerPartyId === viewer && n.discovered !== false && (n.assetId || "USD") === assetId && (p.id === "retail" || n.status !== "spent"));
  const owned = new Set(known.map(n => n.leafId));
  const selection = visible.find(l => l.id === state.leafId) || [...visible].reverse().find(l => owned.has(l.id)) || visible.at(-1);
  const opening = known.find(n => n.leafId === selection?.id);
  const levelY = level => 112 + (depth - level) * 90;
  const nodeX = (level, index) => width * ((index + .5) * 2 ** level / slots);
  let paths = "", nodes = "";
  for (let level = depth; level >= 0; level--) {
    const nodeCount = Math.ceil(Math.max(visible.length, count ? 1 : 2) / 2 ** level);
    for (let i = 0; i < nodeCount; i++) {
      const index = (start >> level) + i;
      const leaf = level === 0 ? leaves[index] : null;
      const value = level === 0 ? leaf?.commitment : tree?.levels[level]?.[index];
      const x = nodeX(level, i), y = levelY(level);
      const latest = level === 0 ? leaf?.sourceTxId === newest : count > 0 && index === ((count - 1) >> level);
      const note = known.find(n => n.leafId === leaf?.id);
      if (level < depth) paths += `<path class="${latest ? "new-path" : ""}" d="M ${nodeX(level + 1, Math.floor(i / 2))} ${levelY(level + 1) + 23} V ${y - 34} H ${x} V ${y - 23}"/>`;
      const title = level === 0 ? leaf ? `Leaf ${index}${note ? note.status === "spent" ? " · spent" : " · yours" : ""}` : "Empty leaf" : `Poseidon · L${level}`;
      nodes += `<g class="retail-tree-node ${leaf ? "leaf-node" : ""} ${leaf && owned.has(leaf.id) ? "owned-leaf" : ""} ${note?.status === "spent" ? "spent-leaf" : ""} ${latest ? "inserted new-path" : ""} ${!value ? "empty-leaf" : ""} ${selection?.id === leaf?.id && leaf ? "selected-leaf" : ""}" data-tree-level="${level}" data-tree-index="${index}" data-node-hash="${value || ""}" transform="translate(${x},${y})" ${leaf ? `id="${elementId}TreeLeaf${index}" data-${prefix}-leaf="${leaf.id}" data-owned="${owned.has(leaf.id)}" data-source-tx="${leaf.sourceTxId}" role="button" tabindex="0" aria-label="Inspect leaf ${index}${owned.has(leaf.id) ? ", recognized by your wallet" : ", commitment"}"` : ""}><title>${value || "Empty subtree"}</title><rect x="-60" y="-25" width="120" height="50" rx="9"/><text y="-5">${title}</text><text y="13" class="hash">${short(value, 6, 4)}</text></g>`;
    }
  }
  const empty = count === 0;
  const pages = Array.from({ length: Math.ceil(count / pageSize) }, (_, page) => {
    const first = page * pageSize, last = Math.min(count, first + pageSize) - 1;
    const recognized = leaves.slice(first, last + 1).filter(l => owned.has(l.id)).length;
    return `<button type="button" id="${elementId}TreePage${page}" data-${prefix}-tree-page="${first}" class="${recognized ? "owned-group" : ""} ${first === lastStart ? "latest-group" : ""}" aria-current="${first === start ? "true" : "false"}" aria-label="Show leaves ${first} to ${last}${recognized ? `, ${recognized} recognized notes` : ""}">${first}–${last}</button>`;
  }).join("");
  return `<section class="retail-tree-panel commitment-explorer" id="${elementId}LiveTree" aria-label="Live commitment tree explorer" data-tree-start="${start}" data-tree-page-size="${pageSize}" data-tree-asset="${assetId}" data-merkle-root="${tree?.root || ""}"><div class="retail-network-head"><div><p class="eyebrow">Live commitment tree · ${vault}</p><h3>${empty ? "Your first note starts here" : `${count} ${count === 1 ? "commitment" : "commitments"}, one shared tree`}</h3></div>${showTraffic ? `<label class="retail-traffic-control"><span class="traffic-dot ${trafficOn ? "on" : ""}"></span><span>Network traffic</span><span class="switch"><input type="checkbox" data-traffic data-traffic-asset="${assetId}" ${trafficOn ? "checked" : ""} aria-label="Network traffic${p.id === "dvp" ? ` for ${assetId}` : ""}"><span></span></span></label>` : ""}</div><div class="retail-tree-legend"><span><i class="purple"></i>${viewer === "public" ? "Public chain · ownership hidden" : `${viewer === "party-0" ? "Your" : `${nameOf(p, viewer)}’s`} notes (${known.length})`}</span><span><i class="gold"></i>New commitments</span><span><i class="grey"></i>Other / empty leaves</span><b>Depth ${treeDepth} · ${count}${capacity ? ` / ${capacity}` : ""} ${count === 1 && !capacity ? "leaf" : "leaves"}</b></div>
    <div class="retail-tree-toolbar"><div class="retail-tree-paging"><button type="button" id="${elementId}TreePrevious" data-${prefix}-tree-move="previous" aria-label="Previous ${pageSize} leaves" ${start === 0 ? "disabled" : ""}>←</button><strong>${empty ? "No leaves yet" : `Leaves ${start}–${start + visible.length - 1}`}</strong><button type="button" id="${elementId}TreeNext" data-${prefix}-tree-move="next" aria-label="Next ${pageSize} leaves" ${start === lastStart ? "disabled" : ""}>→</button></div><span>Up to ${pageSize} leaves per group · one shared root</span><label class="retail-tree-follow"><input type="checkbox" id="${elementId}TreeFollow" data-${prefix}-tree-follow ${state.treeFollow ? "checked" : ""}>Follow latest</label></div>
    ${pages ? `<nav class="retail-tree-map" aria-label="Jump to a leaf group">${pages}</nav>` : ""}
    <div class="retail-tree-scroll" tabindex="0" role="region" aria-label="Tree branches. Scroll horizontally on smaller screens."><svg class="retail-tree-svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}" aria-label="${assetId} commitment tree. Showing ${visible.length} of ${count} leaves. Select a leaf to inspect it."><g class="retail-tree-edges"><path class="${count && start === lastStart ? "new-path" : ""}" d="M ${width / 2} 50 V 75 H ${nodeX(depth, 0)} V 88"/>${paths}</g><g class="retail-tree-root" transform="translate(${width / 2},26)"><rect x="${-Math.min(300, width - 16) / 2}" y="-24" width="${Math.min(300, width - 16)}" height="48" rx="12"/><text y="-5">${assetId} MERKLE ROOT</text><text y="12" class="hash">${tree ? short(tree.root, 18, 8) : "Waiting for the first deposit"}</text></g>${nodes}<text class="retail-collapsed-path" x="${nodeX(depth, 0) + (width < 400 ? 25 : 65)}" y="77">${treeDepth > depth ? `${treeDepth - depth} upper levels folded` : ""}</text></svg></div>
    <div class="retail-leaf-inspector" data-leaf-inspector>${selection ? `<div><small>SELECTED · LEAF ${leaves.indexOf(selection)}</small><strong>${opening ? `${opening.ownerName} · ${fmt(opening.amount)} ${assetId}` : "Sealed note commitment"}</strong><code>${selection.commitment}</code></div><div><small>${opening ? "PRIVATE WALLET OPENING" : "PUBLIC DATA"}</small>${opening ? `<span>Salt ${short(opening.salt, 13, 6)}</span><span>${opening.status === "spent" ? "Spent · nullifier published" : "Unspent note"}</span>` : `<span>Owner and amount hidden</span>`}<span>Source ${short(selection.sourceTxId, 13, 6)}</span></div>` : `<div><small>EMPTY TREE</small><strong>Shield a note to watch its commitment arrive.</strong><span>Each additional note gets a separate leaf.</span></div>`}</div>
    <div class="retail-tree-hint">${state.treeFollow ? "Following new commitments. Choose a group to inspect older leaves." : "Staying on this group as the tree grows. Turn on Follow latest to return to new commitments."}${viewer !== "public" ? " Purple groups contain notes recognized by this wallet." : ""}</div>
    <div class="retail-chain-ticker" aria-label="Recent tree insertions">${p.transactions.filter(t => leaves.some(l => l.sourceTxId === t.id)).slice(0, 3).map(t => `<span><b>${t.background ? "NETWORK" : "WALLET"}</b>${t.type.includes("shield") ? "Deposit" : "Payment"}<code>${short(t.id, 9, 4)}</code><strong>+${leaves.filter(l => l.sourceTxId === t.id).length} ${leaves.filter(l => l.sourceTxId === t.id).length === 1 ? "leaf" : "leaves"}</strong></span>`).join("") || `<span>Deposits and payments will appear here as they enter the tree.</span>`}</div></section>`;
}

export function noteTreeClick(target, p) {
  if (target.closest("[data-note-tree-show]")) {
    const panel = document.querySelector(".commitment-explorer");
    panel?.scrollIntoView({ block: "start", behavior: "instant" });
    panel?.querySelector(".retail-tree-scroll")?.focus({ preventScroll: true });
    return false;
  }
  const panel = target.closest(".commitment-explorer");
  if (!panel) return false;
  const assetId = panel.dataset.treeAsset, state = noteTreeState(p, assetId), size = treePageSize();
  const leaves = p.leaves.filter(leaf => p.trees?.[assetId]?.leafIds.includes(leaf.id));
  const group = target.closest("[data-note-tree-page]"), move = target.closest("[data-note-tree-move]")?.dataset.noteTreeMove;
  const leaf = target.closest("[data-note-leaf]")?.dataset.noteLeaf;
  if (group || move) {
    const last = Math.floor(Math.max(0, leaves.length - 1) / size) * size;
    state.treeStart = Math.max(0, Math.min(last, group ? Number(group.dataset.noteTreePage) : Number(panel.dataset.treeStart) + (move === "next" ? size : -size)));
    state.leafId = "";
  } else if (leaf) {
    state.leafId = leaf;
    state.treeStart = Math.floor(leaves.findIndex(item => item.id === leaf) / size) * size;
  } else return false;
  state.treeFollow = false;
  return true;
}

export function noteTreeChange(target, p) {
  if (!target.matches("[data-note-tree-follow]")) return false;
  const panel = target.closest(".commitment-explorer"), state = noteTreeState(p, panel.dataset.treeAsset);
  state.treeFollow = target.checked;
  state.treeStart = Number(panel.dataset.treeStart);
  state.leafId = "";
  return true;
}
