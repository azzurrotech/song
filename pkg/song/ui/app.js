/* song admin — vanilla JS, no frameworks, no CDNs. */
"use strict";

const state = {
	silo: localStorage.getItem("song_silo") || "",
	dir: "",
	mode: "new", // "new" | "edit"
	editingTemplate: "",
};

const $ = (id) => document.getElementById(id);
const api = (path, opts = {}) => fetch(path, opts).then(async (res) => {
	const body = res.status === 204 ? null : await res.json().catch(() => null);
	if (!res.ok) throw new Error((body && body.error) || ("HTTP " + res.status));
	return body;
});
const qs = (obj) => new URLSearchParams(obj).toString();
const esc = (s) => String(s).replace(/[&<>"']/g, (c) =>
	({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));

function setStatus(kind, msg) {
	const el = $("status");
	el.textContent = msg || (kind === "ok" ? "ok" : kind === "err" ? "error" : "…");
	el.className = "status " + (kind || "");
}

/* ---- tabs ---- */
document.querySelectorAll(".tab").forEach((btn) => {
	btn.addEventListener("click", () => {
		document.querySelectorAll(".tab").forEach((b) => b.classList.remove("active"));
		document.querySelectorAll(".panel").forEach((p) => p.classList.remove("active"));
		btn.classList.add("active");
		$("tab-" + btn.dataset.tab).classList.add("active");
		if (btn.dataset.tab === "templates") loadTemplates();
		if (btn.dataset.tab === "meta") loadMeta();
	});
});

/* ---- silos ---- */
async function loadSilos() {
	const { silos } = await api("/api/song/silos");
	const sel = $("silo-select");
	sel.innerHTML = "";
	if (!silos.length) {
		sel.innerHTML = '<option value="">(no silos yet)</option>';
		return;
	}
	silos.forEach((s) => {
		const opt = document.createElement("option");
		opt.value = s.name;
		opt.textContent = s.name + " (" + s.files + " files)";
		sel.appendChild(opt);
	});
	if (state.silo && silos.some((s) => s.name === state.silo)) sel.value = state.silo;
	else sel.value = silos[0].name;
	selectSilo(sel.value);
}

function selectSilo(name) {
	state.silo = name;
	localStorage.setItem("song_silo", name);
	$("open-site").href = "/" + encodeURIComponent(name) + "/";
	state.dir = "";
	renderCrumbs();
	loadFiles();
}

$("silo-select").addEventListener("change", (e) => selectSilo(e.target.value));

$("new-silo-form").addEventListener("submit", async (e) => {
	e.preventDefault();
	const name = $("new-silo-name").value.trim();
	try {
		await api("/api/song/silos", {
			method: "POST",
			headers: { "Content-Type": "application/x-www-form-urlencoded" },
			body: qs({ name }),
		});
		$("new-silo-name").value = "";
		await loadSilos();
		selectSilo(name);
		setStatus("ok", "silo created");
	} catch (err) { setStatus("err", err.message); }
});

$("delete-silo").addEventListener("click", async () => {
	if (!state.silo || !confirm("Delete silo '" + state.silo + "' and ALL its files?")) return;
	try {
		await api("/api/song/silos/" + encodeURIComponent(state.silo), { method: "DELETE" });
		await loadSilos();
		setStatus("ok", "silo deleted");
	} catch (err) { setStatus("err", err.message); }
});

/* ---- file browser ---- */
function renderCrumbs() {
	$("current-path").textContent = "/" + state.silo + (state.dir ? "/" + state.dir : "") + "/";
}

async function loadFiles() {
	if (!state.silo) return;
	try {
		const { files } = await api("/api/song/silos/" + encodeURIComponent(state.silo) +
			"/files?path=" + encodeURIComponent(state.dir));
		const tbody = $("file-table").querySelector("tbody");
		tbody.innerHTML = "";
		files.forEach((f) => {
			const tr = document.createElement("tr");
			const name = document.createElement("td");
			name.className = "name" + (f.is_dir ? " dir" : "");
			name.textContent = f.is_dir ? f.name + "/" : f.name;
			name.title = f.path;
			name.addEventListener("click", () => {
				if (f.is_dir) {
					state.dir = f.path.replace(/^\//, "");
					renderCrumbs();
					loadFiles();
				} else {
					editFile(f.path);
				}
			});
			const size = document.createElement("td");
			size.textContent = f.is_dir ? "—" : f.size.toLocaleString();
			const type = document.createElement("td");
			type.textContent = f.is_dir ? "dir" : (f.content_type || "");
			const enc = document.createElement("td");
			enc.innerHTML = f.encrypted ? '<span class="tag">encrypted</span>' : "";
			const mod = document.createElement("td");
			mod.textContent = new Date(f.modified).toLocaleString();
			const acts = document.createElement("td");
			acts.className = "actions";
			if (!f.is_dir) {
				const del = document.createElement("button");
				del.className = "button danger";
				del.textContent = "delete";
				del.addEventListener("click", async () => {
					if (!confirm("Delete " + f.path + "?")) return;
					try {
						await api("/api/song/silos/" + encodeURIComponent(state.silo) +
							"/file?path=" + encodeURIComponent(state.dir + "/" + f.name), { method: "DELETE" });
						await loadFiles();
					} catch (err) { setStatus("err", err.message); }
				});
				acts.appendChild(del);
			}
			tr.appendChild(name); tr.appendChild(size); tr.appendChild(type);
			tr.appendChild(enc); tr.appendChild(mod); tr.appendChild(acts);
			tbody.appendChild(tr);
		});
	} catch (err) { setStatus("err", err.message); }
}

$("nav-up").addEventListener("click", () => {
	const parts = state.dir.split("/").filter(Boolean);
	parts.pop();
	state.dir = parts.join("/");
	renderCrumbs();
	loadFiles();
});

$("new-file-btn").addEventListener("click", () => showFileForm("new"));
$("file-cancel").addEventListener("click", () => $("file-form").classList.add("hidden"));

function showFileForm(mode, pathOrName) {
	state.mode = mode;
	$("file-form").classList.remove("hidden");
	if (mode === "new") {
		$("file-path-input").value = state.dir ? state.dir + "/" : "";
		$("file-content").value = "";
		$("file-encrypt").checked = false;
		$("file-overwrite").checked = false;
		$("file-mode").value = "new";
		$("file-save").textContent = "create";
	} else {
		$("file-path-input").value = pathOrName;
		$("file-mode").value = "edit";
		$("file-save").textContent = "update";
		loadFileContent(pathOrName);
	}
}

async function loadFileContent(rel) {
	try {
		const data = await api("/api/song/silos/" + encodeURIComponent(state.silo) +
			"/file?path=" + encodeURIComponent(rel) + "&content=1");
		$("file-content").value = data.content || "";
		$("file-encrypt").checked = !!data.encrypted;
		$("file-overwrite").checked = true;
	} catch (err) { setStatus("err", err.message); }
}

$("file-form").addEventListener("submit", async (e) => {
	e.preventDefault();
	const path = $("file-path-input").value.trim();
	if (!path) return;
	const body = qs({
		path,
		content: $("file-content").value,
		encrypt: $("file-encrypt").checked ? "true" : "false",
		overwrite: $("file-overwrite").checked ? "true" : "false",
	});
	try {
		await api("/api/song/silos/" + encodeURIComponent(state.silo) + "/file", {
			method: state.mode === "edit" ? "PUT" : "POST",
			headers: { "Content-Type": "application/x-www-form-urlencoded" },
			body,
		});
		$("file-form").classList.add("hidden");
		await loadFiles();
		setStatus("ok", "saved " + path);
	} catch (err) { setStatus("err", err.message); }
});

/* ---- templates & SSR ---- */
async function loadTemplates() {
	if (!state.silo) return;
	try {
		const { names, files } = await api("/api/song/silos/" + encodeURIComponent(state.silo) + "/templates");
		const sel = $("template-select");
		sel.innerHTML = "";
		(names || []).forEach((n) => {
			const opt = document.createElement("option");
			opt.value = n;
			opt.textContent = n;
			sel.appendChild(opt);
		});
		state.templateFiles = files || [];
		if (files.length) updateTemplateSource(files[0].name);
		else $("template-source").textContent = "(no .templates/ files — add *.tmpl via the Files tab, path .templates/page.tmpl)";
	} catch (err) { setStatus("err", err.message); }
}

function updateTemplateSource(name) {
	const f = (state.templateFiles || []).find((x) => x.name === name);
	$("template-source").textContent = f ? f.content : "(source not available)";
}

$("template-select").addEventListener("change", (e) => {
	$("template-source").textContent = "(loading…)";
	updateTemplateSource(e.target.value);
});
$("template-reload").addEventListener("click", () => {
	$("template-source").textContent = "(loading…)";
	loadTemplates();
});

$("render-form").addEventListener("submit", async (e) => {
	e.preventDefault();
	const tpl = $("render-template").value.trim();
	const raw = $("render-data").value.trim();
	let payload;
	if (raw) {
		try { payload = JSON.parse(raw); }
		catch (err) { $("render-frame").srcdoc = "<pre>invalid JSON: " + esc(err.message) + "</pre>"; return; }
		payload = { template: tpl, data: payload };
	} else {
		payload = { template: tpl, data: {} };
	}
	try {
		const res = await fetch("/api/song/silos/" + encodeURIComponent(state.silo) + "/render", {
			method: "POST",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(payload),
		});
		if (!res.ok) {
			const body = await res.json().catch(() => null);
			throw new Error((body && body.error) || ("HTTP " + res.status));
		}
		const html = await res.text();
		$("render-frame").srcdoc = html;
		setStatus("ok", "rendered " + tpl);
	} catch (err) { setStatus("err", err.message); }
});

/* ---- meta ---- */
async function loadMeta() {
	if (!state.silo) return;
	try {
		const m = await api("/api/song/silos/" + encodeURIComponent(state.silo) + "/meta");
		$("meta-json").value = JSON.stringify(m, null, 2);
	} catch (err) { setStatus("err", err.message); }
}

$("meta-form").addEventListener("submit", async (e) => {
	e.preventDefault();
	let m;
	try { m = JSON.parse($("meta-json").value); }
	catch (err) { $("meta-status").textContent = "invalid JSON: " + err.message; return; }
	try {
		await api("/api/song/silos/" + encodeURIComponent(state.silo) + "/meta", {
			method: "PUT",
			headers: { "Content-Type": "application/json" },
			body: JSON.stringify(m),
		});
		$("meta-status").textContent = "meta saved — index rules & template routes are live";
	} catch (err) { setStatus("err", err.message); }
});

/* ---- upload ---- */
$("upload-form").addEventListener("submit", async (e) => {
	e.preventDefault();
	const files = $("upload-files").files;
	if (!files.length || !state.silo) return;
	const fd = new FormData();
	fd.set("dir", $("upload-dir").value.trim());
	fd.set("encrypt", $("upload-encrypt").checked ? "true" : "false");
	[...files].forEach((f) => fd.append("file", f));
	const list = $("upload-results");
	list.innerHTML = "<li>uploading…</li>";
	try {
		const { uploads } = await api("/api/song/silos/" + encodeURIComponent(state.silo) + "/upload", {
			method: "POST",
			body: fd,
		});
		list.innerHTML = "";
		uploads.forEach((u) => {
			const li = document.createElement("li");
			li.textContent = (u.error ? "✗ " : "✓ ") + u.path + (u.error ? " — " + u.error : "");
			list.appendChild(li);
		});
		await loadFiles();
	} catch (err) { setStatus("err", err.message); }
});

/* ---- boot ---- */
loadSilos().catch((err) => setStatus("err", err.message));