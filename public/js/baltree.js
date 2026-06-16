// Renders the indented output of a ledger `bal` query as a collapsible tree.
// The raw report text is read from #balTreeRaw; each line's amount is already
// ledger's rolled-up subtotal, so we just reconstruct the hierarchy from the
// indentation and display it. See mockups/bal_tree.html for the prototype.
(function () {
  "use strict";

  // A row's account name appears on its final physical line; rows with extra
  // currencies print amount-only lines first. The name's start column encodes
  // depth (ledger indents a fixed number of spaces per level).
  var AMOUNT_ONLY = /^\s*(US\$|\$)\s*(-?[\d,]+\.\d+)\s*$/;
  var AMOUNT_NAME = /^(\s*(?:US\$|\$)\s*-?[\d,]+\.\d+)(\s+)(\S.*?)\s*$/;
  var SLOTS = ["$", "US$"]; // fixed currency columns so amounts line up

  function parseAmount(cur, num) {
    return { cur: cur, val: parseFloat(num.replace(/,/g, "")) };
  }

  function parseBal(raw) {
    var lines = raw.split("\n");
    var rows = [];
    var pending = [];
    var afterRule = false;
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i];
      if (/^-{3,}\s*$/.test(line)) { afterRule = true; pending = []; continue; }
      if (line.trim() === "") continue;

      var m = line.match(AMOUNT_NAME);
      if (m) {
        var amounts = pending.slice();
        var a = m[1].match(/(US\$|\$)\s*(-?[\d,]+\.\d+)/);
        amounts.push(parseAmount(a[1], a[2]));
        pending = [];
        rows.push({ nameStart: m[1].length + m[2].length, name: m[3], amounts: amounts });
        continue;
      }
      m = line.match(AMOUNT_ONLY);
      if (m && !afterRule) pending.push(parseAmount(m[1], m[2]));
      // amount-only lines after the dashed rule are the grand total — ignored,
      // since the top-level node already shows it.
    }
    if (!rows.length) return { roots: [], maxDepth: 0 };

    var nameStarts = rows.map(function (r) { return r.nameStart; });
    var minStart = Math.min.apply(null, nameStarts);
    var uniq = nameStarts.filter(function (v, idx, arr) { return arr.indexOf(v) === idx; })
                         .sort(function (x, y) { return x - y; });
    var unit = uniq.length > 1 ? uniq[1] - uniq[0] : 2;

    var roots = [], stack = [], maxDepth = 0;
    for (var j = 0; j < rows.length; j++) {
      var r = rows[j];
      var depth = Math.round((r.nameStart - minStart) / unit);
      if (depth > maxDepth) maxDepth = depth;
      var node = { name: r.name, amounts: r.amounts, children: [], depth: depth };
      if (depth === 0 || !stack[depth - 1]) roots.push(node);
      else stack[depth - 1].children.push(node);
      stack[depth] = node;
      stack.length = depth + 1;
    }
    return { roots: roots, maxDepth: maxDepth };
  }

  function fmtVal(val) {
    return val.toLocaleString("en-US", { minimumFractionDigits: 2, maximumFractionDigits: 2 });
  }
  function amountsHtml(amounts) {
    return SLOTS.map(function (cur) {
      var found = amounts.filter(function (x) { return x.cur === cur; })[0];
      var usd = cur === "US$";
      var txt = found ? cur + " " + fmtVal(found.val) : "";
      return '<span class="bal-amt' + (usd ? " usd" : "") + '">' + txt + "</span>";
    }).join("");
  }

  function renderNode(node) {
    var li = document.createElement("li");
    li.dataset.depth = node.depth;
    var hasChildren = node.children.length > 0;

    var div = document.createElement("div");
    div.className = "bal-node";

    var amounts = document.createElement("span");
    amounts.className = "bal-amounts";
    amounts.innerHTML = amountsHtml(node.amounts);
    div.appendChild(amounts);

    var label = document.createElement("span");
    label.className = "bal-label";
    label.style.paddingLeft = (18 + node.depth * 18) + "px";

    var twisty = document.createElement("span");
    twisty.className = "bal-twisty" + (hasChildren ? "" : " leaf");
    twisty.textContent = hasChildren ? "▾" : "•";
    label.appendChild(twisty);

    var name = document.createElement("span");
    name.className = "bal-name";
    name.textContent = node.name;
    label.appendChild(name);

    div.appendChild(label);
    li.appendChild(div);

    if (hasChildren) {
      var ul = document.createElement("ul");
      for (var i = 0; i < node.children.length; i++) ul.appendChild(renderNode(node.children[i]));
      li.appendChild(ul);
      div.style.cursor = "pointer";
      div.onclick = function () {
        li.classList.toggle("collapsed");
        twisty.textContent = li.classList.contains("collapsed") ? "▸" : "▾";
      };
    }
    return li;
  }

  var maxDepth = 0, level = 1, treeEl = null, indicatorEl = null;

  function applyLevel() {
    var items = treeEl.querySelectorAll("li");
    for (var i = 0; i < items.length; i++) {
      var li = items[i];
      var tw = li.querySelector(":scope > .bal-node .bal-twisty");
      if (!tw || tw.classList.contains("leaf")) continue;
      var collapsed = (+li.dataset.depth) >= level;
      li.classList.toggle("collapsed", collapsed);
      tw.textContent = collapsed ? "▸" : "▾";
    }
    if (indicatorEl) {
      indicatorEl.textContent = "showing " + level + " of " + (maxDepth + 1) +
        (maxDepth ? " levels" : " level");
    }
  }

  window.balTree = {
    setLevel: function (n) { level = Math.max(1, Math.min(n, maxDepth + 1)); applyLevel(); },
    changeLevel: function (d) { this.setLevel(level + d); },
    topLevel: function () { this.setLevel(1); },
    expandAll: function () { this.setLevel(maxDepth + 1); },
    showView: function (which) {
      var tree = which === "tree";
      document.getElementById("balTree").classList.toggle("hidden", !tree);
      document.getElementById("balTreeRaw").classList.toggle("hidden", tree);
      document.getElementById("balBtnTree").classList.toggle("active", tree);
      document.getElementById("balBtnRaw").classList.toggle("active", !tree);
    }
  };

  function init() {
    treeEl = document.getElementById("balTree");
    indicatorEl = document.getElementById("balDepthIndicator");
    var rawEl = document.getElementById("balTreeRaw");
    if (!treeEl || !rawEl) return;
    var parsed = parseBal(rawEl.textContent);
    maxDepth = parsed.maxDepth;
    for (var i = 0; i < parsed.roots.length; i++) treeEl.appendChild(renderNode(parsed.roots[i]));
    balTree.expandAll(); // fully expanded by default
  }

  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", init);
  else init();
})();
