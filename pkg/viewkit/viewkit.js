// song viewkit — dependency-free view decisions shared by song-hosted apps.
//
// This file is the one place for the client-side logic every app repeats:
// escaping, stable ids, relative time, the hash router, the dashboard
// aggregation, the equirectangular map projection and clustering, severity
// triage and the annotation counts a timeline shows inline.
//
// It is deliberately DOM-free and framework-free. A page loads it once with
//
//   <script src="/_song/viewkit.js" defer></script>
//
// and gets `window.SongView`. Node's require() gets the same object, so the
// decisions can be tested without a browser.
(function (root, factory) {
  const api = factory();
  if (typeof module !== "undefined" && module.exports) module.exports = api;
  root.SongView = api;
})(typeof globalThis !== "undefined" ? globalThis : this, function () {
  "use strict";

  // --- text ----------------------------------------------------------------

  const ESC = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

  // escapeHTML is for the few places that build text rather than DOM nodes.
  // Rendered text goes through textContent instead.
  function escapeHTML(s) {
    return String(s == null ? "" : s).replace(/[&<>"']/g, (c) => ESC[c]);
  }

  // domEventName turns an el() attribute key like "onClick" into the event name
  // addEventListener expects.
  function domEventName(key) {
    const s = String(key == null ? "" : key);
    return (s.startsWith("on") ? s.slice(2) : s).toLowerCase();
  }

  // domSafeId makes an arbitrary value safe inside an HTML id attribute.
  function domSafeId(value) {
    const s = String(value == null ? "" : value)
      .replace(/[^A-Za-z0-9_-]+/g, "-")
      .replace(/^-+|-+$/g, "");
    return s || "x";
  }

  // actionId builds a unique, stable element id for a generated control:
  // actionId("comment", "item-1a2b") === "act-comment-item-1a2b".
  function actionId(kind, key) {
    return "act-" + domSafeId(kind) + "-" + domSafeId(key);
  }

  function formatCount(n) {
    const v = Number(n) || 0;
    if (v >= 1e6) return (v / 1e6).toFixed(1).replace(/\.0$/, "") + "M";
    if (v >= 1e3) return (v / 1e3).toFixed(1).replace(/\.0$/, "") + "k";
    return String(v);
  }

  // timeAgo renders "just now", "12m ago", "3h ago", "5d ago" or a date.
  function timeAgo(iso, now) {
    if (!iso) return "—";
    const t = Date.parse(iso);
    if (isNaN(t)) return "—";
    const base = now ? (now instanceof Date ? now.getTime() : Date.parse(now)) : Date.now();
    let s = Math.floor((base - t) / 1000);
    if (isNaN(s)) return "—";
    if (s < 0) s = 0;
    if (s < 45) return "just now";
    if (s < 3600) return Math.max(1, Math.round(s / 60)) + "m ago";
    if (s < 86400) return Math.round(s / 3600) + "h ago";
    if (s < 30 * 86400) return Math.round(s / 86400) + "d ago";
    return new Date(t).toISOString().slice(0, 10);
  }

  function niceDate(iso) {
    if (!iso) return "—";
    const t = Date.parse(iso);
    if (isNaN(t)) return "—";
    return new Date(t).toISOString().replace("T", " ").slice(0, 16) + "Z";
  }

  // --- the router ----------------------------------------------------------

  // routeOf turns "#/timeline?collection=news" into {view, params}. An empty
  // hash is the dashboard, so the app has a home without a redirect.
  function routeOf(hash) {
    const raw = String(hash == null ? "" : hash).replace(/^#\/?/, "");
    const cut = raw.indexOf("?");
    const path = cut < 0 ? raw : raw.slice(0, cut);
    const qs = cut < 0 ? "" : raw.slice(cut + 1);
    const params = {};
    if (qs) {
      for (const pair of qs.split("&")) {
        if (!pair) continue;
        const eq = pair.indexOf("=");
        const k = decodeURIComponent(eq < 0 ? pair : pair.slice(0, eq));
        const v = eq < 0 ? "" : decodeURIComponent(pair.slice(eq + 1).replace(/\+/g, " "));
        params[k] = v;
      }
    }
    const view = (path.split("/")[0] || "dashboard").trim() || "dashboard";
    return { view, params };
  }

  function hrefFor(view, params) {
    const qs = buildQuery(params || {});
    return "#/" + view + (qs ? "?" + qs : "");
  }

  // itemHref is the canonical "show me this record" link. Every view that
  // names a record uses it, so a record is always opened the same way: in the
  // timeline, scrolled to and highlighted. `view` defaults to "timeline";
  // extra parameters are merged without letting the caller drop the id.
  function itemHref(view, id, extra) {
    if (id == null || id === "") return hrefFor(view || "timeline", extra || {});
    const params = Object.assign({}, extra || {}, { item: String(id) });
    return hrefFor(view || "timeline", params);
  }

  function buildQuery(params) {
    const parts = [];
    for (const k of Object.keys(params || {}).sort()) {
      const v = params[k];
      if (v === undefined || v === null || v === "") continue;
      parts.push(encodeURIComponent(k) + "=" + encodeURIComponent(String(v)));
    }
    return parts.join("&");
  }

  function quoteValue(v) {
    const s = String(v == null ? "" : v).trim();
    if (s === "") return "";
    return /[\s"]/.test(s) ? '"' + s.replace(/"/g, '\\"') + '"' : s;
  }

  // --- aggregation for dashboards -----------------------------------------

  function dayOf(item) {
    const stamp = item.published || item.fetched || "";
    return String(stamp).slice(0, 10);
  }

  // aggregate is what three charts read: records per source (bar), per day
  // (line) and per category (pie). It is the common shape of a dashboard.
  function aggregate(items) {
    const bySource = {};
    const byCategory = {};
    const byDay = {};
    for (const it of items || []) {
      const src = it.source || "(unknown)";
      bySource[src] = (bySource[src] || 0) + 1;
      for (const c of it.categories || []) {
        const k = String(c || "").toLowerCase();
        if (k) byCategory[k] = (byCategory[k] || 0) + 1;
      }
      const d = dayOf(it);
      if (d) byDay[d] = (byDay[d] || 0) + 1;
    }
    return { bySource, byCategory, byDay };
  }

  function distinctValues(items, pick) {
    const seen = new Set();
    for (const it of items || []) {
      const v = pick ? pick(it) : it.source;
      if (v) seen.add(v);
    }
    return [...seen].sort();
  }

  function distinctSources(items) {
    return distinctValues(items, (it) => it.source);
  }

  function newestItem(items) {
    let best = null;
    for (const it of items || []) {
      const t = Date.parse(it.published || it.fetched || "");
      if (isNaN(t)) continue;
      if (!best || t > best.t) best = { t, item: it };
    }
    return best ? best.item : null;
  }

  // topEntries turns a {name: count} map into [[name, count]] sorted by count
  // (then name), which is what a chart axis wants.
  function topEntries(map, limit) {
    const out = Object.keys(map || {}).map((k) => [k, map[k]]);
    out.sort((a, b) => (b[1] - a[1]) || a[0].localeCompare(b[0]));
    return limit && limit > 0 ? out.slice(0, limit) : out;
  }

  // seriesOfDay fills the gaps so the line chart has an even x axis.
  function seriesOfDay(byDay) {
    const days = Object.keys(byDay || {}).sort();
    if (days.length === 0) return [];
    const out = [];
    const start = Date.parse(days[0] + "T00:00:00Z");
    const end = Date.parse(days[days.length - 1] + "T00:00:00Z");
    for (let t = start; t <= end; t += 86400000) {
      const d = new Date(t).toISOString().slice(0, 10);
      out.push([d, byDay[d] || 0]);
    }
    return out;
  }

  // --- the map -------------------------------------------------------------

  // project is the equirectangular projection onto a w×h canvas.
  function project(lat, lon, w, h) {
    return { x: ((Number(lon) + 180) / 360) * w, y: ((90 - Number(lat)) / 180) * h };
  }

  function geoPoints(items, w, h) {
    const out = [];
    for (const it of items || []) {
      if (!it.hasGeo) continue;
      const lat = Number(it.lat);
      const lon = Number(it.lon);
      if (isNaN(lat) || isNaN(lon)) continue;
      const p = project(lat, lon, w, h);
      out.push({ item: it, x: p.x, y: p.y });
    }
    return out;
  }

  // cluster groups points into grid cells of `radius` pixels and returns the
  // centroid of each cell.
  function cluster(points, radius) {
    const r = radius > 0 ? radius : 1;
    const cells = new Map();
    const out = [];
    for (const p of points || []) {
      const key = Math.floor(p.x / r) + "," + Math.floor(p.y / r);
      let c = cells.get(key);
      if (!c) {
        c = { x: 0, y: 0, items: [] };
        cells.set(key, c);
        out.push(c);
      }
      c.items.push(p.item);
      c.x += p.x;
      c.y += p.y;
    }
    for (const c of out) {
      c.x /= c.items.length;
      c.y /= c.items.length;
      c.count = c.items.length;
    }
    return out;
  }

  // --- severity triage -----------------------------------------------------

  const SEVERITY = { critical: 4, high: 3, medium: 2, low: 1, info: 0 };

  function severityRank(s) {
    const k = String(s || "").toLowerCase();
    return Object.prototype.hasOwnProperty.call(SEVERITY, k) ? SEVERITY[k] : 0;
  }

  // triage sorts a queue: most severe first, then the most recent.
  function triage(insights) {
    return (insights || []).slice().sort((a, b) => {
      const d = severityRank(b.severity) - severityRank(a.severity);
      if (d !== 0) return d;
      return Date.parse(b.updated || b.created || 0) - Date.parse(a.updated || a.created || 0);
    });
  }

  // --- graph helpers -------------------------------------------------------

  function indexByID(items) {
    const m = new Map();
    for (const it of items || []) m.set(it.id, it);
    return m;
  }

  // referenceEnds resolves a link record to the *other* record for a given
  // item, whichever direction the link was written in. Views use it so an
  // incoming link and an outgoing link render identically.
  function referenceEnds(link, itemID) {
    if (!link) return { from: "", to: "", other: "" };
    if (link.from === itemID) return { from: link.from, to: link.to, other: link.to };
    if (link.to === itemID) return { from: link.from, to: link.to, other: link.from };
    return { from: link.from, to: link.to, other: link.to || link.from };
  }

  // annotations counts, per item, the analyst's own marks so a timeline can
  // show them inline.
  function annotations(items, links, comments, notes) {
    const out = new Map();
    const bump = (id, key) => {
      if (!id) return;
      const cur = out.get(id) || { links: 0, comments: 0, notes: 0 };
      cur[key]++;
      out.set(id, cur);
    };
    for (const l of links || []) {
      bump(l.from, "links");
      bump(l.to, "links");
    }
    for (const c of comments || []) bump(c.item, "comments");
    for (const n of notes || []) bump(n.subject, "notes");
    for (const it of items || []) {
      if (!out.has(it.id)) out.set(it.id, { links: 0, comments: 0, notes: 0 });
    }
    return out;
  }

  // annotationSections normalises the three kinds of analyst marks a record can
  // carry (comments, notes and links) into one ordered, renderable shape, so
  // every view renders them the same way and a new record type only has to name
  // its fields:
  //
  //   [{ kind: "comments", title: "Comments", count: 1, items: [
  //       { id, head: "analyst", body, when, meta }] }, …]
  //
  // A link is rendered from the *other* record's title, whichever direction the
  // edge was written in; opts.linkTitle(item) supplies the label.
  function annotationSections(rec, opts) {
    rec = rec || {};
    opts = opts || {};
    const linkTitle = typeof opts.linkTitle === "function"
      ? opts.linkTitle
      : (l) => l.title || l.other || l.to || l.from || "(link)";
    const out = [];

    const comments = (rec.comments || []).filter(Boolean).map((c) => ({
      id: c.id || "",
      head: c.author || "anonymous",
      body: c.body || "",
      when: c.created || "",
      meta: "",
    }));
    if (comments.length) out.push({ kind: "comments", title: "Comments", count: comments.length, items: comments });

    const notes = (rec.notes || []).filter(Boolean).map((n) => ({
      id: n.id || "",
      head: n.title || "(untitled note)",
      body: n.body || "",
      when: n.updated || n.created || "",
      meta: (n.tags || []).join(", "),
    }));
    if (notes.length) out.push({ kind: "notes", title: "Notes", count: notes.length, items: notes });

    const links = (rec.links || []).filter(Boolean).map((l) => {
      const other = referenceEnds(l, rec.id).other;
      return {
        id: l.id || "",
        head: linkTitle(l, other),
        body: l.note || "",
        when: l.created || "",
        meta: [l.relation || "related", l.origin || ""].filter(Boolean).join(" · "),
        other: other,
        incoming: l.to === rec.id,
        relation: l.relation || "related",
        origin: l.origin || "",
      };
    });
    if (links.length) out.push({ kind: "links", title: "Links", count: links.length, items: links });

    return out;
  }

  // timelineGroups splits a newest-first item list into consecutive day
  // sections — the shape a timeline header needs. It never reorders the list.
  function timelineGroups(items) {
    const out = [];
    const byDay = new Map();
    for (const it of items || []) {
      const day = dayOf(it) || "";
      let g = byDay.get(day);
      if (!g) {
        g = { day: day, label: day || "undated", items: [] };
        byDay.set(day, g);
        out.push(g);
      }
      g.items.push(it);
    }
    return out;
  }

  return {
    escapeHTML: escapeHTML,
    domEventName: domEventName,
    domSafeId: domSafeId,
    actionId: actionId,
    formatCount: formatCount,
    timeAgo: timeAgo,
    niceDate: niceDate,
    routeOf: routeOf,
    hrefFor: hrefFor,
    itemHref: itemHref,
    buildQuery: buildQuery,
    quoteValue: quoteValue,
    dayOf: dayOf,
    aggregate: aggregate,
    distinctValues: distinctValues,
    distinctSources: distinctSources,
    newestItem: newestItem,
    topEntries: topEntries,
    seriesOfDay: seriesOfDay,
    project: project,
    geoPoints: geoPoints,
    cluster: cluster,
    severityRank: severityRank,
    triage: triage,
    indexByID: indexByID,
    referenceEnds: referenceEnds,
    annotations: annotations,
    annotationSections: annotationSections,
    timelineGroups: timelineGroups,
  };
});
