/* signpost — the graph viewer.
 *
 * Hand-written, no dependencies, per ADR 0008. The reason that is affordable is
 * the measured size of the thing being drawn: 23 nodes and 27 edges on this
 * repository, three node kinds, two edge kinds. A force layout over that is the
 * `layout` function below and nothing else.
 *
 * The security rule this file follows, from design §7.2: every string here came
 * out of a repository and is therefore untrusted. There is no `innerHTML`, no
 * `insertAdjacentHTML`, and no template that concatenates a repository string
 * into markup — nodes are built with createElement/createElementNS and text is
 * always set through textContent. The CSP in graph.html is the backstop, not the
 * defence.
 */

(function () {
  "use strict";

  var SVG = "http://www.w3.org/2000/svg";

  // The plot's own coordinate space. The SVG scales to its container through
  // viewBox, so nothing here depends on the rendered pixel size.
  var W = 1000;

  // The height a repository of signpost's own shape gets, and the height layout()
  // grows from. It is a floor rather than a fixed size: a repository with enough
  // edgeless nodes needs a band taller than this, and the alternative to growing
  // was subtracting a band bigger than the frame and rendering those nodes at
  // negative coordinates, outside the viewBox, where they are not drawn at all.
  var H_BASE = 660;

  // Set by layout() to H_BASE or more. Everything downstream — the viewBox, the
  // pan clamp, the arrow-key step, the pixel-to-unit conversion — reads it, so the
  // taller frame is consistent by construction rather than by remembering to
  // update each of them.
  var H = H_BASE;

  // What the connected part of the graph keeps even when the band has taken
  // everything else. Below roughly this the cells are too short to lay a component
  // out in, and 2 * PAD of it is padding.
  var MAIN_MIN = 260;

  var PAD = 46;

  // Node kinds signpost emits, in the order they should appear in the legend:
  // what the repository is, then what it says about itself, then what it depends
  // on. Anything unrecognised still renders — a kind added to the generator must
  // not silently vanish from the view — it just lands in the last group.
  var KINDS = [
    { id: "Module", label: "modules" },
    { id: "Document", label: "documents" },
    { id: "External Dependency", label: "dependencies" },
  ];

  var EDGES = [
    { id: "imports", label: "imports" },
    { id: "co_changes", label: "co-change" },
  ];

  // Where a file in the detail panel can be read, with a trailing slash, or "" for
  // nowhere. The page states it: graph.html hardcodes this repository, and `signpost
  // view` sets it from -repo or leaves it off. Read from the document rather than
  // written here because this file now serves both, and a hardcoded URL sent every
  // file link in every other repository to a 404 in *this* one.
  var REPO_BASE = repoBase();

  // A URL out of the document is untrusted like everything else here, so it is checked
  // against the one shape that is a forge blob root rather than escaped. An href is a
  // navigation target: `javascript:` in this string would run on click, and the CSP
  // does not stop that.
  function repoBase() {
    var raw = document.documentElement.getAttribute("data-repo-base");
    if (!raw) {
      return "";
    }
    // Anchored, https only, and no character that could close the authority early or
    // start a new component: a host, a path of plain segments, and a trailing slash.
    if (!/^https:\/\/[a-z0-9.-]+\/[A-Za-z0-9._~/-]*\/$/.test(raw)) {
      return "";
    }
    return raw;
  }

  var el = {
    plot: document.querySelector("[data-plot]"),
    fallback: document.querySelector("[data-fallback]"),
    controls: document.querySelector("[data-controls]"),
    kindFilters: document.querySelector("[data-kind-filters]"),
    edgeFilters: document.querySelector("[data-edge-filters]"),
    count: document.querySelector("[data-count]"),
    detail: document.querySelector("[data-detail]"),
    detailHint: document.querySelector("[data-detail-hint]"),
    detailBody: document.querySelector("[data-detail-body]"),
    detailTitle: document.querySelector("[data-detail-title]"),
    detailKind: document.querySelector("[data-detail-kind]"),
    detailDesc: document.querySelector("[data-detail-desc]"),
    detailAttrs: document.querySelector("[data-detail-attrs]"),
    detailFiles: document.querySelector("[data-detail-files]"),
    detailFileList: document.querySelector("[data-detail-filelist]"),
    detailEdges: document.querySelector("[data-detail-edges]"),
    detailEdgeList: document.querySelector("[data-detail-edgelist]"),
    zoom: document.querySelector("[data-zoom]"),
    zoomIn: document.querySelector("[data-zoom-in]"),
    zoomOut: document.querySelector("[data-zoom-out]"),
    zoomReset: document.querySelector("[data-zoom-reset]"),
    zoomLevel: document.querySelector("[data-zoom-level]"),
  };

  var state = {
    nodes: [],
    edges: [],
    byID: {},
    kindOn: {},
    edgeOn: {},
    selected: null,
    // Set by layout(): how the frame was divided between the connected graph and
    // the band of nodes with no edges.
    zones: null,
    // The view transform, in viewBox units: a point of the drawing at g lands at
    // view.x + view.k * g. Held here rather than in render() because render()
    // rebuilds the whole svg, and a filter toggle that threw away the reader's
    // position would make the filters unusable at any zoom above 1.
    view: { k: 1, x: 0, y: 0 },
    // The <g> currently carrying that transform, so a drag can move the picture
    // without rebuilding 25 nodes per pointer event.
    viewG: null,
    drag: null,
    // Set when a drag actually moved, and read by the node click handler: a drag
    // that starts on a node must not also select it.
    suppressClick: false,
  };

  fetch("graph.json", { credentials: "omit" })
    .then(function (res) {
      if (!res.ok) {
        throw new Error("graph.json responded " + res.status);
      }
      return res.json();
    })
    .then(init)
    .catch(failed);

  // failed says what to run instead. A viewer that cannot load its data is a
  // dead end only if it does not name the path that does not need it.
  function failed(err) {
    clear(el.fallback);
    el.fallback.appendChild(
      text("The graph could not be loaded: " + err.message + ". ")
    );
    el.fallback.appendChild(text("It is readable without this page — "));
    el.fallback.appendChild(mono("signpost graph ."));
    el.fallback.appendChild(text(" prints the findings as text."));
  }

  function init(data) {
    var nodes = Array.isArray(data.nodes) ? data.nodes : [];
    var edges = Array.isArray(data.edges) ? data.edges : [];

    state.nodes = nodes.map(function (n, i) {
      return {
        id: String(n.id || "n" + i),
        kind: String(n.kind || "Module"),
        title: String(n.title || n.id || ""),
        description: n.description ? String(n.description) : "",
        path: n.path ? String(n.path) : "",
        lang: n.lang ? String(n.lang) : "",
        attrs: n.attrs && typeof n.attrs === "object" ? n.attrs : {},
        files: Array.isArray(n.files) ? n.files.map(String) : [],
        cluster: typeof n.cluster === "number" ? n.cluster : 0,
        degree: 0,
        x: 0,
        y: 0,
      };
    });

    state.nodes.forEach(function (n) {
      state.byID[n.id] = n;
    });

    // An edge naming a node that is not present is dropped rather than drawn to
    // an invented one. The generator already enforces this; the viewer does not
    // get to assume it, because the JSON is untrusted input.
    state.edges = edges
      .filter(function (e) {
        return state.byID[String(e.from)] && state.byID[String(e.to)];
      })
      .map(function (e) {
        return {
          from: String(e.from),
          to: String(e.to),
          kind: String(e.kind || "imports"),
          conf: String(e.confidence || "extracted"),
          weight: typeof e.weight === "number" ? e.weight : 0,
          source: e.source ? String(e.source) : "",
        };
      });

    state.edges.forEach(function (e) {
      state.byID[e.from].degree += 1;
      state.byID[e.to].degree += 1;
    });

    kindsPresent().forEach(function (k) {
      state.kindOn[k] = true;
    });
    edgeKindsPresent().forEach(function (k) {
      state.edgeOn[k] = true;
    });

    layout();
    buildFilters();
    bindZoomControls();
    el.controls.hidden = false;
    el.detail.hidden = false;
    render();
  }

  /* --- layout -------------------------------------------------------------- */

  // A seeded generator, not Math.random: the same graph has to produce the same
  // picture on every load. Someone who reloads the page and sees a different
  // arrangement cannot tell whether the repository changed, and "same input,
  // same output" is the property the whole tool is built on.
  function rng(seed) {
    var s = seed >>> 0;
    return function () {
      s = (s * 1664525 + 1013904223) >>> 0;
      return s / 4294967296;
    };
  }

  // The label is truncated to a width the frame can hold, because the alternative
  // is what the first draft of this file did: labels that overlapped each other
  // and ran off the edge of the viewBox. The full string is on the node's <title>
  // and in the detail panel, so nothing is lost, only shortened.
  var LABEL_MAX = 22;

  // Measured off the rendered page rather than assumed: `.plot__label` is 11px
  // Spline Sans Mono, whose advance came out at 6.60–6.67 units per character, so
  // `internal/assemble` is about 113 units wide. Rounded up, because the fallback
  // when the webfont has not loaded is the platform monospace and that is not
  // guaranteed to be narrower. Every spacing decision below is expressed in terms
  // of this: the thing that must not overlap is the label, not the dot.
  var CHAR_W = 6.8;

  // The label's baseline sits this far below the node's rim, and 11px text
  // descends about 3 units below its own baseline.
  var LABEL_DY = 14;
  var LABEL_DESC = 3;

  // Clear space demanded between two node footprints.
  var GAP = 7;

  // How far the axes may be scaled differently from each other when a component
  // is fitted into its cell. See normalise().
  var STRETCH_MAX = 2.6;

  // layout is two layouts, because this graph is two different things.
  //
  // A single force pass over everything is what the first version did, and it
  // fails on exactly this shape: 10 nodes that import each other and 15 that have
  // no edges at all. Repulsion has nothing to balance it for an edgeless node, so
  // the isolated ones get pushed to the frame edge, the connected ones collapse
  // into whatever corner is left, and rescaling afterwards shrinks the only part
  // of the picture that carries structure.
  //
  // So the connected components are laid out with forces and given the main area,
  // and the nodes with no edges are placed in a grid below them. That is not a
  // cosmetic split: "signpost read no edges here" is a finding, and a tidy row of
  // them says it, where a scatter around the rim looks like a layout accident.
  function layout() {
    if (state.nodes.length === 0) {
      return;
    }

    var comps = components();
    var linked = comps.filter(function (c) {
      return c.length > 1;
    });
    var alone = comps
      .filter(function (c) {
        return c.length === 1;
      })
      .map(function (c) {
        return c[0];
      });

    // The band for the edgeless nodes is sized to what it holds, so a repository
    // where everything is connected gives the whole frame to the graph.
    var cols = Math.min(5, Math.max(1, alone.length));
    var gridRows = Math.ceil(alone.length / cols);
    var band = alone.length ? gridRows * 52 + 26 : 0;
    var split = band ? 18 : 0;

    // The frame grows rather than the band eating the graph. Sixteen rows of
    // edgeless nodes need 858 units of band, which is more than the 660 the frame
    // starts with; subtracting that gave a negative main area, and from there
    // negative cell heights, a band rule at y=-208, and a quarter of the nodes
    // placed off-canvas where the browser simply does not draw them. Growing keeps
    // the drawing correct at the cost of a taller picture, which is the right way
    // round: the page scales the SVG to its container either way.
    //
    // It grows for the connected part too, and for the same reason. `separate` can
    // only push nodes apart into space that exists: sixteen six-node components
    // give each a cell 191 by 108 units, which holds three footprints of a
    // long-named module and is asked to hold six, and no pass budget resolves that.
    // Measured before this: 64 overlapping label pairs.
    var mainH = Math.max(MAIN_MIN, H_BASE - band - split, mainNeeded(linked));
    H = mainH + band + split;

    // The ids, not the count: the caption has to say how many are *drawn*, and a
    // filter changes that. A number fixed at layout time would keep claiming 16
    // while the band showed 7.
    state.zones = {
      mainH: mainH,
      band: band,
      alone: alone.map(function (n) {
        return n.id;
      }),
    };

    placeLinked(linked, mainH);
    placeAlone(alone, cols, mainH + (band ? 18 : 0));
  }

  // Undirected connected components. Sorted by size descending, and within a size
  // by node id, so the packing order does not depend on map iteration order.
  function components() {
    var seen = {};
    var adj = {};
    state.nodes.forEach(function (n) {
      adj[n.id] = [];
    });
    state.edges.forEach(function (e) {
      adj[e.from].push(e.to);
      adj[e.to].push(e.from);
    });

    var out = [];
    state.nodes.forEach(function (n) {
      if (seen[n.id]) {
        return;
      }
      var comp = [];
      var stack = [n.id];
      seen[n.id] = true;
      while (stack.length) {
        var id = stack.pop();
        comp.push(state.byID[id]);
        adj[id].forEach(function (next) {
          if (!seen[next]) {
            seen[next] = true;
            stack.push(next);
          }
        });
      }
      comp.sort(function (a, b) {
        return a.id < b.id ? -1 : 1;
      });
      out.push(comp);
    });

    out.sort(function (a, b) {
      if (a.length !== b.length) {
        return b.length - a.length;
      }
      return a[0].id < b[0].id ? -1 : 1;
    });
    return out;
  }

  /* How the components are arranged into a grid of cells.
   *
   * One function, called by both mainNeeded and placeLinked, because the first
   * answers "how tall must the frame be for this grid" and the second answers
   * "where does each node go in it". Two copies of this arithmetic that drifted
   * apart would mean a height computed for a layout that is not the one drawn, and
   * a square-root of the component count in each was exactly that risk.
   *
   * Square-ish, then narrowed until a cell is at least as wide as the widest
   * footprint it has to hold. The frame cannot grow sideways — W is the page's
   * width — so columns are the constrained axis and rows are the free one: at 100
   * components, ten columns gave each cell 55 units for a 136-unit label and no
   * arrangement inside it could be made legible. Fewer, wider columns and more
   * rows can be. */
  function grid(comps) {
    var widest = 0;
    comps.forEach(function (comp) {
      comp.forEach(function (n) {
        widest = Math.max(widest, 2 * halfW(n) + GAP);
      });
    });

    // The cell inset placeLinked applies, so this compares like with like.
    var fits = Math.floor((W - 2 * PAD) / (widest + 36));
    var cols = Math.min(Math.ceil(Math.sqrt(comps.length)), Math.max(1, fits));
    return { cols: cols, rows: Math.ceil(comps.length / cols) };
  }

  /* The main area's height, if every component is to have room for its nodes'
   * footprints without overlap.
   *
   * A grid of footprints is a lower bound rather than a prediction: the force pass
   * does not place nodes on a grid, and separate() moves them from wherever it
   * does place them. So this is padded, and separate() remains the thing that
   * actually guarantees no overlap. What this removes is the case where no
   * arrangement could have. */
  function mainNeeded(comps) {
    if (comps.length === 0) {
      return 0;
    }
    var g = grid(comps);
    var bw = (W - 2 * PAD) / g.cols - 36;

    var tallest = 0;
    comps.forEach(function (comp) {
      var fw = 0;
      var fh = 0;
      comp.forEach(function (n) {
        fw = Math.max(fw, 2 * halfW(n) + GAP);
        fh = Math.max(fh, 2 * halfH(n) + GAP);
      });
      // At least one per row even when a single footprint is wider than the cell:
      // that node is already as clamped as contain() can make it, and dividing by
      // a non-positive number here would put NaN or Infinity into every
      // coordinate. grid() makes this the exception rather than the common case.
      var across = bw > 0 && fw > 0 ? Math.max(1, Math.floor((bw + GAP) / fw)) : 1;
      var deep = Math.ceil(comp.length / across);
      // A quarter more than the grid needs, because the nodes do not arrive on one.
      tallest = Math.max(tallest, deep * fh * 1.25);
    });

    return Math.ceil(g.rows * (tallest + 34) + 2 * PAD);
  }

  // Each component is laid out on its own and then normalised into a cell. One
  // component takes the whole area; several share a grid. Packing them tighter
  // than a grid would buy space this graph does not need.
  function placeLinked(comps, mainH) {
    if (comps.length === 0) {
      return;
    }
    var g = grid(comps);
    var cols = g.cols;
    var rows = g.rows;
    var cellW = (W - 2 * PAD) / cols;
    var cellH = (mainH - 2 * PAD) / rows;

    comps.forEach(function (comp, i) {
      force(comp, i);
      var cx = PAD + (i % cols) * cellW;
      var cy = PAD + Math.floor(i / cols) * cellH;
      // The inset keeps a component's labels inside its own cell rather than
      // running into the neighbouring one.
      var bx = cx + 18;
      var by = cy + 10;
      var bw = cellW - 36;
      var bh = cellH - 34;
      normalise(comp, bx, by, bw, bh);
      separate(comp, bx, by, bw, bh);
    });
  }

  /* The footprint of a node — the box its dot and its label occupy together.
   *
   * Spacing is decided in these terms rather than in distances between centres,
   * which is the distinction the first version of this file missed. Two nodes 52
   * units apart are comfortably separated as dots and unreadable as labels, and
   * the label is the part carrying the name. */
  function halfW(n) {
    return Math.max(radius(n), (shorten(n.title).length * CHAR_W) / 2);
  }

  function halfH(n) {
    return radius(n) + (LABEL_DY + LABEL_DESC) / 2;
  }

  // The footprint's vertical centre is below the dot's, because the label hangs
  // underneath it.
  function midY(n) {
    return n.y + (LABEL_DY + LABEL_DESC) / 2;
  }

  // A grid, ordered the way the legend is: modules, then documents, then
  // dependencies, and alphabetically inside each. An arbitrary order here would
  // make the band look like leftovers rather than a category.
  function placeAlone(nodes, cols, top) {
    if (nodes.length === 0) {
      return;
    }
    var order = kindsPresent();
    nodes.sort(function (a, b) {
      var ka = order.indexOf(a.kind);
      var kb = order.indexOf(b.kind);
      if (ka !== kb) {
        return ka - kb;
      }
      return a.title < b.title ? -1 : 1;
    });

    var cellW = (W - 2 * PAD) / cols;
    nodes.forEach(function (n, i) {
      n.x = PAD + (i % cols) * cellW + cellW / 2;
      n.y = top + 30 + Math.floor(i / cols) * 52;
    });
  }

  // Fruchterman-Reingold, run on one component in an arbitrary local frame; the
  // caller normalises the result into place. Repulsion between every pair,
  // attraction along edges, a cooling step. At this scale the O(n²) pass is a few
  // thousand operations, so there is no quadtree and no reason for one.
  function force(comp, seed) {
    var n = comp.length;
    var index = {};
    comp.forEach(function (node, i) {
      index[node.id] = i;
    });

    var rand = rng(0x51617e + seed * 7919);
    var side = 600;
    var k = Math.sqrt((side * side) / n);

    // Seeded on a circle rather than uniformly: a random scatter regularly starts
    // two nodes almost coincident, and the repulsion term then throws them to the
    // edge of the frame where they stay.
    comp.forEach(function (node, i) {
      var a = (i / n) * Math.PI * 2;
      var r = (side / 3) * (0.55 + 0.45 * rand());
      node.x = side / 2 + Math.cos(a) * r;
      node.y = side / 2 + Math.sin(a) * r;
    });

    var inner = state.edges.filter(function (e) {
      return index[e.from] !== undefined && index[e.to] !== undefined;
    });

    var iters = 320;
    var temp = side / 8;
    var cool = temp / (iters + 1);

    for (var step = 0; step < iters; step++) {
      var dx = new Float64Array(n);
      var dy = new Float64Array(n);

      for (var i = 0; i < n; i++) {
        for (var j = i + 1; j < n; j++) {
          var vx = comp[i].x - comp[j].x;
          var vy = comp[i].y - comp[j].y;
          var d2 = vx * vx + vy * vy;
          if (d2 < 0.01) {
            // Coincident: nudge deterministically so the pair separates without
            // introducing per-load randomness.
            vx = 0.1 * (i % 2 === 0 ? 1 : -1);
            vy = 0.1;
            d2 = vx * vx + vy * vy;
          }
          var d = Math.sqrt(d2);
          var rep = (k * k) / d;
          dx[i] += (vx / d) * rep;
          dy[i] += (vy / d) * rep;
          dx[j] -= (vx / d) * rep;
          dy[j] -= (vy / d) * rep;
        }
      }

      inner.forEach(function (e) {
        var ia = index[e.from];
        var ib = index[e.to];
        var vx = comp[ia].x - comp[ib].x;
        var vy = comp[ia].y - comp[ib].y;
        var d = Math.sqrt(vx * vx + vy * vy) || 0.01;
        var att = (d * d) / k;
        dx[ia] -= (vx / d) * att;
        dy[ia] -= (vy / d) * att;
        dx[ib] += (vx / d) * att;
        dy[ib] += (vy / d) * att;
      });

      for (var m = 0; m < n; m++) {
        var len = Math.sqrt(dx[m] * dx[m] + dy[m] * dy[m]) || 0.01;
        var lim = Math.min(len, temp);
        comp[m].x += (dx[m] / len) * lim;
        comp[m].y += (dy[m] / len) * lim;
      }
      temp -= cool;
    }
  }

  // Fit a component's settled coordinates into a box.
  //
  // The two axes are scaled separately, within a bound. Preserving aspect is what
  // the first version did, and on this repository it cost most of the frame: the
  // force pass settles a component roughly square, the cell is far wider than it
  // is tall, so the height bound and 680 of the 1000 units of width went unused —
  // ten nodes crowded into a third of the picture.
  //
  // Scaling the axes apart is legitimate here specifically because `force` has no
  // notion of label size: it spaces centres, so the aspect ratio it happens to
  // settle into is not a fact about the graph and stretching does not destroy one.
  // What it does carry is which nodes are near which, and that survives. The bound
  // is there because an unbounded stretch would flatten a component into a line
  // the moment the cell is much wider than tall.
  function normalise(comp, bx, by, bw, bh) {
    var xs = comp.map(function (n) {
      return n.x;
    });
    var ys = comp.map(function (n) {
      return n.y;
    });
    var minX = Math.min.apply(null, xs);
    var maxX = Math.max.apply(null, xs);
    var minY = Math.min.apply(null, ys);
    var maxY = Math.max.apply(null, ys);
    var spanX = Math.max(1, maxX - minX);
    var spanY = Math.max(1, maxY - minY);

    var fitX = bw / spanX;
    var fitY = bh / spanY;
    var fit = Math.min(fitX, fitY);
    var sx = Math.min(fitX, fit * STRETCH_MAX);
    var sy = Math.min(fitY, fit * STRETCH_MAX);

    // Centred in the box: the axis that hit the stretch bound still has slack, and
    // leaving it all on one side would push the drawing against an edge.
    var offX = bx + (bw - spanX * sx) / 2;
    var offY = by + (bh - spanY * sy) / 2;

    comp.forEach(function (n) {
      n.x = offX + (n.x - minX) * sx;
      n.y = offY + (n.y - minY) * sy;
      contain(n, bx, by, bw, bh);
    });
  }

  /* Push overlapping footprints apart until none overlap, or the pass budget runs
   * out.
   *
   * This is the guarantee, and normalise is only the thing that makes it cheap to
   * satisfy. Spreading a component across the frame leaves most pairs clear, but
   * "most" is not a property worth shipping: whether two labels collide depends on
   * how long they are, and the force pass never looked. So overlap is measured
   * directly, on the footprint boxes, and resolved.
   *
   * A pair is separated along whichever axis is closer to clear — measured as a
   * fraction of what that axis needs, not in units, so a pair that is nearly clear
   * horizontally is not shoved vertically past three other nodes to fix it. */
  function separate(comp, bx, by, bw, bh) {
    var n = comp.length;
    for (var pass = 0; pass < 90; pass++) {
      var overlapping = false;
      for (var i = 0; i < n; i++) {
        for (var j = i + 1; j < n; j++) {
          var a = comp[i];
          var b = comp[j];
          var needX = halfW(a) + halfW(b) + GAP;
          var needY = halfH(a) + halfH(b) + GAP;
          var vx = b.x - a.x;
          var vy = midY(b) - midY(a);
          var overX = needX - Math.abs(vx);
          var overY = needY - Math.abs(vy);
          // Clear on either axis is clear: these are boxes, not circles.
          if (overX <= 0 || overY <= 0) {
            continue;
          }
          overlapping = true;
          if (overX / needX <= overY / needY) {
            // Coincident on this axis: part them deterministically rather than
            // with a random jitter, so the same graph still draws the same way.
            var dirX = vx === 0 ? (i % 2 === 0 ? 1 : -1) : vx > 0 ? 1 : -1;
            a.x -= (dirX * overX) / 2;
            b.x += (dirX * overX) / 2;
          } else {
            var dirY = vy === 0 ? (i % 2 === 0 ? 1 : -1) : vy > 0 ? 1 : -1;
            a.y -= (dirY * overY) / 2;
            b.y += (dirY * overY) / 2;
          }
          contain(a, bx, by, bw, bh);
          contain(b, bx, by, bw, bh);
        }
      }
      if (!overlapping) {
        return;
      }
    }
  }

  /* Hold a node inside its box, footprint included. This is what keeps a label
   * from being clipped by the frame or from running into the neighbouring cell.
   *
   * A label wider than the box it is in cannot satisfy both edges, so it is
   * centred instead — off the edge by the same amount on both sides beats hard
   * against one of them. */
  function contain(n, bx, by, bw, bh) {
    var hw = halfW(n);
    var hh = halfH(n);
    n.x = 2 * hw >= bw ? bx + bw / 2 : Math.max(bx + hw, Math.min(bx + bw - hw, n.x));
    // The dot's own y, offset back out of the footprint's centre.
    var off = (LABEL_DY + LABEL_DESC) / 2;
    n.y =
      2 * hh >= bh
        ? by + bh / 2 - off
        : Math.max(by + hh, Math.min(by + bh - hh, midY(n))) - off;
  }

  /* --- filters ------------------------------------------------------------- */

  function kindsPresent() {
    return present(
      KINDS.map(function (k) {
        return k.id;
      }),
      state.nodes.map(function (n) {
        return n.kind;
      })
    );
  }

  function edgeKindsPresent() {
    return present(
      EDGES.map(function (e) {
        return e.id;
      }),
      state.edges.map(function (e) {
        return e.kind;
      })
    );
  }

  // present orders the known values first and appends anything unknown, so a new
  // kind in the generator shows up in the UI on the next deploy without this file
  // being touched.
  function present(known, seen) {
    var out = [];
    known.forEach(function (k) {
      if (seen.indexOf(k) !== -1) {
        out.push(k);
      }
    });
    seen.forEach(function (s) {
      if (out.indexOf(s) === -1) {
        out.push(s);
      }
    });
    return out;
  }

  function labelFor(list, id) {
    for (var i = 0; i < list.length; i++) {
      if (list[i].id === id) {
        return list[i].label;
      }
    }
    return id;
  }

  function buildFilters() {
    kindsPresent().forEach(function (k) {
      el.kindFilters.appendChild(
        toggle(labelFor(KINDS, k), countNodes(k), slug(k), function (on) {
          state.kindOn[k] = on;
          render();
        })
      );
    });
    edgeKindsPresent().forEach(function (k) {
      el.edgeFilters.appendChild(
        toggle(labelFor(EDGES, k), countEdges(k), slug(k), function (on) {
          state.edgeOn[k] = on;
          render();
        })
      );
    });
  }

  function toggle(label, n, mark, onChange) {
    var wrap = document.createElement("label");
    wrap.className = "ctl__item";
    wrap.setAttribute("data-mark", mark);

    var box = document.createElement("input");
    box.type = "checkbox";
    box.checked = true;
    box.className = "ctl__box";
    box.addEventListener("change", function () {
      onChange(box.checked);
    });

    var swatch = document.createElement("span");
    swatch.className = "ctl__swatch";
    swatch.setAttribute("aria-hidden", "true");

    var name = document.createElement("span");
    name.className = "ctl__name";
    name.textContent = label;

    var num = document.createElement("span");
    num.className = "ctl__num";
    num.textContent = String(n);

    wrap.appendChild(box);
    wrap.appendChild(swatch);
    wrap.appendChild(name);
    wrap.appendChild(num);
    return wrap;
  }

  function countNodes(kind) {
    return state.nodes.filter(function (n) {
      return n.kind === kind;
    }).length;
  }

  function countEdges(kind) {
    return state.edges.filter(function (e) {
      return e.kind === kind;
    }).length;
  }

  function visibleNodes() {
    return state.nodes.filter(function (n) {
      return state.kindOn[n.kind];
    });
  }

  function visibleEdges() {
    return state.edges.filter(function (e) {
      return (
        state.edgeOn[e.kind] &&
        state.kindOn[state.byID[e.from].kind] &&
        state.kindOn[state.byID[e.to].kind]
      );
    });
  }

  /* --- render -------------------------------------------------------------- */

  function render() {
    var nodes = visibleNodes();
    var edges = visibleEdges();

    el.count.textContent =
      nodes.length +
      " of " +
      state.nodes.length +
      " nodes, " +
      edges.length +
      " of " +
      state.edges.length +
      " edges";

    clear(el.plot);

    var svg = document.createElementNS(SVG, "svg");
    svg.setAttribute("viewBox", "0 0 " + W + " " + H);
    svg.setAttribute("class", "plot__svg");
    svg.setAttribute("role", "img");
    svg.setAttribute(
      "aria-label",
      "Node-link diagram: " + nodes.length + " nodes, " + edges.length + " edges"
    );

    svg.appendChild(arrowDefs());

    // Everything drawn goes inside one group, and zooming and panning is that
    // group's transform. Nothing else in the file knows the view exists: node and
    // edge coordinates stay in layout space, so what a reader sees at 3× is the
    // same picture magnified rather than a second layout that might disagree with
    // the first.
    var view = document.createElementNS(SVG, "g");
    view.setAttribute("class", "plot__view");
    state.viewG = view;
    svg.appendChild(view);

    var aloneShown = state.zones
      ? state.zones.alone.filter(function (id) {
          return state.kindOn[state.byID[id].kind];
        }).length
      : 0;
    if (aloneShown > 0) {
      view.appendChild(bandRule(aloneShown));
    }

    var gEdges = document.createElementNS(SVG, "g");
    gEdges.setAttribute("class", "plot__edges");
    edges.forEach(function (e) {
      gEdges.appendChild(edgeLine(e));
    });
    view.appendChild(gEdges);

    var gNodes = document.createElementNS(SVG, "g");
    gNodes.setAttribute("class", "plot__nodes");
    nodes.forEach(function (n) {
      gNodes.appendChild(nodeMark(n));
    });
    view.appendChild(gNodes);

    applyView();
    bindView(svg);
    el.plot.appendChild(svg);
  }

  /* --- zoom and pan --------------------------------------------------------- */

  // 1 is the fitted view. Below it the labels stop being readable, and past 6×
  // there is nothing left to look at on a graph this size.
  var ZOOM_MIN = 1;
  var ZOOM_MAX = 6;
  var ZOOM_STEP = 1.35;

  function applyView() {
    if (!state.viewG) {
      return;
    }
    var v = state.view;
    state.viewG.setAttribute(
      "transform",
      "translate(" + fixed(v.x) + " " + fixed(v.y) + ") scale(" + fixed(v.k) + ")"
    );
    // classList on an SVGElement is supported everywhere the rest of this file is.
    state.viewG.parentNode.classList.toggle("is-pannable", v.k > ZOOM_MIN);
    if (el.zoomLevel) {
      el.zoomLevel.textContent = Math.round(v.k * 100) + "%";
    }
    if (el.zoomReset) {
      el.zoomReset.disabled = v.k === 1 && v.x === 0 && v.y === 0;
    }
    if (el.zoomIn) {
      el.zoomIn.disabled = v.k >= ZOOM_MAX;
    }
    if (el.zoomOut) {
      el.zoomOut.disabled = v.k <= ZOOM_MIN;
    }
  }

  // Zoom about a fixed point of the drawing, so what is under the cursor stays
  // under the cursor. Zooming about the origin instead is what makes a viewer feel
  // like it is fighting you: the thing you were looking at slides off-screen.
  function zoomAbout(k, px, py) {
    var v = state.view;
    var next = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, k));
    if (next === v.k) {
      return;
    }
    v.x = px - ((px - v.x) * next) / v.k;
    v.y = py - ((py - v.y) * next) / v.k;
    v.k = next;
    clampView();
    applyView();
  }

  // At every zoom level the frame stays covered by the drawing. Panning to empty
  // space is the other way a hand-rolled viewer goes wrong — a reader who drags
  // too far is left looking at nothing with no indication of which way back.
  function clampView() {
    var v = state.view;
    var slackX = W * (v.k - 1);
    var slackY = H * (v.k - 1);
    v.x = Math.max(-slackX, Math.min(0, v.x));
    v.y = Math.max(-slackY, Math.min(0, v.y));
  }

  function zoomBy(factor) {
    // No pointer involved, so hold the centre of the frame.
    var v = state.view;
    zoomAbout(v.k * factor, (W / 2 - v.x) / v.k, (H / 2 - v.y) / v.k);
  }

  function resetView() {
    state.view = { k: 1, x: 0, y: 0 };
    applyView();
  }

  // Client pixels to viewBox units. The svg scales to its container, so the ratio
  // is whatever the current rendered width happens to be — reading it from the
  // element rather than assuming it is what makes wheel-zoom land where the cursor
  // is at every window size.
  function toPlot(svg, ev) {
    var r = svg.getBoundingClientRect();
    if (r.width === 0 || r.height === 0) {
      return { x: W / 2, y: H / 2 };
    }
    var v = state.view;
    return {
      x: (((ev.clientX - r.left) / r.width) * W - v.x) / v.k,
      y: (((ev.clientY - r.top) / r.height) * H - v.y) / v.k,
    };
  }

  function bindView(svg) {
    svg.addEventListener(
      "wheel",
      function (ev) {
        // Only claim the wheel while zoomed in or zooming in. Swallowing it at 1×
        // on a downward scroll would trap the page scroll on a graph that has
        // nowhere to go.
        var into = ev.deltaY < 0;
        if (!into && state.view.k <= ZOOM_MIN) {
          return;
        }
        ev.preventDefault();
        var p = toPlot(svg, ev);
        zoomAbout(state.view.k * (into ? ZOOM_STEP : 1 / ZOOM_STEP), p.x, p.y);
      },
      { passive: false }
    );

    svg.addEventListener("pointerdown", function (ev) {
      // Cleared here rather than after the guard below, and before the node click
      // handler gets a chance to read it: a drag that ends over empty space sets
      // the flag and no node click ever consumes it, so it must be disarmed by
      // the next press whether or not that press can start a drag. Clearing it
      // after the guard left click-to-select broken at 1× after any pan.
      state.suppressClick = false;
      if (!ev.isPrimary || state.view.k <= ZOOM_MIN) {
        return;
      }
      state.drag = {
        id: ev.pointerId,
        cx: ev.clientX,
        cy: ev.clientY,
        x: state.view.x,
        y: state.view.y,
        moved: false,
      };
      svg.classList.add("is-panning");
      // Capture keeps the drag alive when the pointer leaves the frame, which is
      // most drags at 6×. It is an improvement rather than a requirement — the
      // svg's own pointermove drives the pan either way — and it throws if the
      // pointer is already gone, so a failure here must not abandon the drag with
      // the class still applied and the graph following an unpressed cursor.
      try {
        svg.setPointerCapture(ev.pointerId);
      } catch (e) {
        /* the drag still works, it just stops at the frame edge */
      }
    });

    svg.addEventListener("pointermove", function (ev) {
      var d = state.drag;
      if (!d || ev.pointerId !== d.id) {
        return;
      }
      var r = svg.getBoundingClientRect();
      if (r.width === 0) {
        return;
      }
      var dx = ev.clientX - d.cx;
      var dy = ev.clientY - d.cy;
      // A few pixels of travel is a click with a shaky hand, not a drag.
      if (!d.moved && Math.abs(dx) + Math.abs(dy) > 3) {
        d.moved = true;
      }
      state.view.x = d.x + (dx / r.width) * W;
      state.view.y = d.y + (dy / r.height) * H;
      clampView();
      applyView();
    });

    ["pointerup", "pointercancel"].forEach(function (kind) {
      svg.addEventListener(kind, function (ev) {
        var d = state.drag;
        if (!d || ev.pointerId !== d.id) {
          return;
        }
        // The click that ends a drag would otherwise select whatever node the
        // pointer came to rest on.
        state.suppressClick = d.moved;
        state.drag = null;
        svg.classList.remove("is-panning");
      });
    });
  }

  // Both mouse gestures need a keyboard equivalent. The wheel gets one for free:
  // a real <button> is operable by Enter and Space with nothing added here. The
  // drag does not, so the arrow keys pan while focus is anywhere in this group —
  // otherwise a reader who zooms in from the keyboard can reach the middle of the
  // graph and nothing else.
  //
  // The controls sit beside the plot rather than inside it because the plot's own
  // focusable children are the nodes, and claiming the arrow keys there would
  // interfere with moving between them.
  function bindZoomControls() {
    if (!el.zoom) {
      return;
    }
    el.zoomIn.addEventListener("click", function () {
      zoomBy(ZOOM_STEP);
    });
    el.zoomOut.addEventListener("click", function () {
      zoomBy(1 / ZOOM_STEP);
    });
    el.zoomReset.addEventListener("click", resetView);

    var STEPS = {
      ArrowLeft: [1, 0],
      ArrowRight: [-1, 0],
      ArrowUp: [0, 1],
      ArrowDown: [0, -1],
    };
    el.zoom.addEventListener("keydown", function (ev) {
      var step = STEPS[ev.key];
      if (!step || state.view.k <= ZOOM_MIN) {
        return;
      }
      ev.preventDefault();
      // A twelfth of the frame per press: enough to cross the picture in a few
      // presses at 2×, small enough to aim with at 6×.
      state.view.x += (step[0] * W) / 12;
      state.view.y += (step[1] * H) / 12;
      clampView();
      applyView();
    });
  }

  // Two markers, because the confidence distinction has to survive into the
  // rendering: an inferred edge is amber and dashed, and its arrowhead has to
  // match or the head would read as a solid claim on a dashed edge.
  function arrowDefs() {
    var defs = document.createElementNS(SVG, "defs");
    defs.appendChild(marker("sp-head", "plot__head"));
    defs.appendChild(marker("sp-head-u", "plot__head plot__head--u"));
    return defs;
  }

  // A rule and a caption above the edgeless band, so the split reads as a
  // statement — signpost found no edges for these — rather than as two groups the
  // layout happened to produce.
  function bandRule(shown) {
    var g = document.createElementNS(SVG, "g");
    var y = state.zones.mainH + 8;

    var line = document.createElementNS(SVG, "line");
    line.setAttribute("x1", PAD);
    line.setAttribute("y1", fixed(y));
    line.setAttribute("x2", W - PAD);
    line.setAttribute("y2", fixed(y));
    line.setAttribute("class", "plot__rule");
    g.appendChild(line);

    var cap = document.createElementNS(SVG, "text");
    cap.setAttribute("x", PAD);
    cap.setAttribute("y", fixed(y + 15));
    cap.setAttribute("class", "plot__band");
    cap.textContent = "no edges read (" + shown + ")";
    g.appendChild(cap);
    return g;
  }

  function marker(id, cls) {
    var m = document.createElementNS(SVG, "marker");
    m.setAttribute("id", id);
    m.setAttribute("viewBox", "0 0 10 10");
    m.setAttribute("refX", "9");
    m.setAttribute("refY", "5");
    m.setAttribute("markerWidth", "6");
    m.setAttribute("markerHeight", "6");
    m.setAttribute("orient", "auto-start-reverse");
    var p = document.createElementNS(SVG, "path");
    p.setAttribute("d", "M 0 1 L 10 5 L 0 9 z");
    p.setAttribute("class", cls);
    m.appendChild(p);
    return m;
  }

  function edgeLine(e) {
    var a = state.byID[e.from];
    var b = state.byID[e.to];
    var line = document.createElementNS(SVG, "line");

    // Stop the line at the target's rim rather than its centre, so the arrowhead
    // sits outside the circle instead of under it.
    var vx = b.x - a.x;
    var vy = b.y - a.y;
    var d = Math.sqrt(vx * vx + vy * vy) || 1;
    var ra = radius(a) + 2;
    var rb = radius(b) + 7;

    line.setAttribute("x1", fixed(a.x + (vx / d) * ra));
    line.setAttribute("y1", fixed(a.y + (vy / d) * ra));
    line.setAttribute("x2", fixed(b.x - (vx / d) * rb));
    line.setAttribute("y2", fixed(b.y - (vy / d) * rb));

    var cls = "plot__edge plot__edge--" + slug(e.kind);
    if (e.conf !== "extracted") {
      cls += " plot__edge--u";
    }
    if (state.selected && (e.from === state.selected || e.to === state.selected)) {
      cls += " is-lit";
    } else if (state.selected) {
      cls += " is-dim";
    }
    line.setAttribute("class", cls);
    // Co-change carries no direction — it means two things changed in the same
    // commits — so it gets no arrowhead. Drawing one would assert a direction the
    // data does not have.
    if (e.kind !== "co_changes") {
      line.setAttribute(
        "marker-end",
        e.conf === "extracted" ? "url(#sp-head)" : "url(#sp-head-u)"
      );
    }
    if (e.weight > 0) {
      line.setAttribute("stroke-width", fixed(Math.min(3.2, 1 + e.weight * 0.28)));
    }

    var t = document.createElementNS(SVG, "title");
    t.textContent = a.title + " " + e.kind.replace(/_/g, " ") + " " + b.title;
    line.appendChild(t);
    return line;
  }

  // Degree sets the radius, so the hubs are the big circles. That is the one
  // structural fact worth encoding in the geometry: it is what a person is
  // looking for when they open a graph of a codebase they do not know.
  function radius(n) {
    return 7 + Math.min(13, Math.sqrt(n.degree) * 4.2);
  }

  function nodeMark(n) {
    var g = document.createElementNS(SVG, "g");
    var cls = "plot__node plot__node--" + slug(n.kind);
    if (state.selected === n.id) {
      cls += " is-sel";
    } else if (state.selected && !adjacent(n.id)) {
      cls += " is-dim";
    }
    g.setAttribute("class", cls);
    g.setAttribute("tabindex", "0");
    g.setAttribute("role", "button");
    g.setAttribute("aria-label", n.title + ", " + n.kind);

    var c = document.createElementNS(SVG, "circle");
    c.setAttribute("cx", fixed(n.x));
    c.setAttribute("cy", fixed(n.y));
    c.setAttribute("r", fixed(radius(n)));
    c.setAttribute("class", "plot__dot");
    g.appendChild(c);

    var label = document.createElementNS(SVG, "text");
    label.setAttribute("x", fixed(n.x));
    label.setAttribute("y", fixed(n.y + radius(n) + 14));
    label.setAttribute("class", "plot__label");
    label.setAttribute("text-anchor", "middle");
    label.textContent = shorten(n.title);
    g.appendChild(label);

    var t = document.createElementNS(SVG, "title");
    t.textContent = n.title + " — " + n.kind;
    g.appendChild(t);

    g.addEventListener("click", function () {
      if (state.suppressClick) {
        state.suppressClick = false;
        return;
      }
      select(n.id);
    });
    g.addEventListener("keydown", function (ev) {
      if (ev.key === "Enter" || ev.key === " ") {
        ev.preventDefault();
        select(n.id);
      }
    });
    return g;
  }

  function adjacent(id) {
    if (state.selected === id) {
      return true;
    }
    return state.edges.some(function (e) {
      return (
        (e.from === state.selected && e.to === id) ||
        (e.to === state.selected && e.from === id)
      );
    });
  }

  /* --- detail panel -------------------------------------------------------- */

  function select(id) {
    state.selected = state.selected === id ? null : id;
    render();
    showDetail();
  }

  function showDetail() {
    if (!state.selected) {
      el.detailHint.hidden = false;
      el.detailBody.hidden = true;
      return;
    }
    var n = state.byID[state.selected];
    el.detailHint.hidden = true;
    el.detailBody.hidden = false;

    el.detailTitle.textContent = n.title;
    el.detailKind.textContent = n.lang ? n.kind + " · " + n.lang : n.kind;
    el.detailDesc.textContent = n.description;

    clear(el.detailAttrs);
    Object.keys(n.attrs)
      .sort()
      .forEach(function (key) {
        var dt = document.createElement("dt");
        dt.textContent = key.replace(/_/g, " ");
        var dd = document.createElement("dd");
        dd.textContent = String(n.attrs[key]);
        el.detailAttrs.appendChild(dt);
        el.detailAttrs.appendChild(dd);
      });

    clear(el.detailFileList);
    el.detailFiles.hidden = n.files.length === 0;
    n.files.forEach(function (f) {
      var li = document.createElement("li");
      // A link when the page says where this repository's files can be read, and the
      // filename as text when it does not. `signpost view` serves an arbitrary
      // repository and often knows no URL for it, so a link is the special case here
      // rather than the default — and a filename that reads as a link and 404s is
      // worse than one that does not offer.
      //
      // The href is a prefix the page supplied plus an encoded path. encodeURI on the
      // repository-supplied path is what keeps a filename from steering the URL; see
      // repoBase for why the prefix itself is restricted to known hosts.
      if (REPO_BASE) {
        var a = document.createElement("a");
        a.href = REPO_BASE + encodeURI(f);
        a.textContent = f;
        a.rel = "noopener";
        li.appendChild(a);
      } else {
        li.appendChild(text(f));
      }
      el.detailFileList.appendChild(li);
    });

    clear(el.detailEdgeList);
    var touching = incident(n.id);
    el.detailEdges.hidden = touching.length === 0;
    touching.forEach(function (row) {
      var li = document.createElement("li");
      li.appendChild(mono(row.both ? "↔" : row.out ? "→" : "←"));
      li.appendChild(text(" " + row.other.title + " "));
      var kind = document.createElement("span");
      kind.className = "detail__edgekind";
      kind.textContent = row.kind.replace(/_/g, " ");
      li.appendChild(kind);
      if (row.conf !== "extracted") {
        var u = document.createElement("span");
        u.className = "detail__u";
        u.textContent = "inferred";
        li.appendChild(u);
      }
      el.detailEdgeList.appendChild(li);
    });
  }

  // Edges touching a node, folded so a relation that exists in both directions is
  // listed once with `↔`.
  //
  // Co-change is symmetric — two files changed in the same commits — and the
  // generator records it as a pair of directed edges. Listing those as a separate
  // `→ okf` and `← okf` reads as two findings and implies a direction the data does
  // not carry. Folding is not hiding: the same neighbour with a genuinely one-way
  // import still shows its arrow, and both rows survive when the kinds differ.
  function incident(id) {
    var rows = [];
    var seen = {};
    state.edges.forEach(function (e) {
      if (e.from !== id && e.to !== id) {
        return;
      }
      var out = e.from === id;
      var other = state.byID[out ? e.to : e.from];
      var key = other.id + "\u0000" + e.kind;
      if (seen[key] !== undefined) {
        // Already recorded from the other end, so this is the reverse of a
        // relation already listed.
        if (rows[seen[key]].out !== out) {
          rows[seen[key]].both = true;
        }
        return;
      }
      seen[key] = rows.length;
      rows.push({
        other: other,
        kind: e.kind,
        conf: e.conf,
        out: out,
        both: false,
      });
    });
    rows.sort(function (a, b) {
      if (a.other.title !== b.other.title) {
        return a.other.title < b.other.title ? -1 : 1;
      }
      return a.kind < b.kind ? -1 : 1;
    });
    return rows;
  }

  /* --- small helpers ------------------------------------------------------- */

  function clear(node) {
    while (node.firstChild) {
      node.removeChild(node.firstChild);
    }
  }

  function text(s) {
    return document.createTextNode(s);
  }

  function mono(s) {
    var c = document.createElement("code");
    c.textContent = s;
    return c;
  }

  // A CSS class or attribute value assembled from a repository string, so it is
  // restricted to a known alphabet rather than escaped. An unrecognised kind
  // gets a usable class name and the default styling.
  function slug(s) {
    return String(s).toLowerCase().replace(/[^a-z0-9]+/g, "-");
  }

  // Keeps the head of the string, which is the part that identifies the node —
  // `internal/extract` and `internal/export` differ at the end, but an ADR title
  // is only recognisable from its front. The full text stays on the <title> and in
  // the detail panel.
  function shorten(s) {
    if (s.length <= LABEL_MAX) {
      return s;
    }
    return s.slice(0, LABEL_MAX - 1) + "…";
  }

  function fixed(n) {
    return Math.round(n * 100) / 100;
  }
})();
