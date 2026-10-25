// Plancia: modalità «Personalizza». Trascinamento (Pointer Events, mouse e
// touch) di preferiti, applicativi dentro una categoria e categorie; puntina
// per fissare; frecce da tastiera. Ogni modifica si salva subito nel DB.
(() => {
	"use strict";

	const KEY = "cruscotto-personalizza"; // ricarica dopo un errore: si torna in modifica
	const LONG_PRESS_MS = 300;            // touch: tenere premuto prima di trascinare
	const SCROLL_PX = 8;                  // touch: movimento prima della pressione lunga = scorrimento
	const MOUSE_PX = 4;                   // mouse: soglia prima di trascinare
	const body = document.body;
	const editing = () => body.classList.contains("personalizza");

	const status = document.createElement("div");
	status.className = "visually-hidden";
	status.setAttribute("role", "status");
	status.setAttribute("aria-live", "polite");
	body.appendChild(status);
	const say = (text) => { status.textContent = ""; setTimeout(() => { status.textContent = text; }, 50); };

	const store = (fn) => { try { return fn(window.sessionStorage); } catch (_) { return null; } };

	// ── Elementi ordinabili ──
	const favGrid = () => document.querySelector('[data-sortable="preferiti"]');
	function items(box) {
		const sel = box.dataset.sortable === "categorie" ? ":scope > .category" : ":scope > .tile";
		return [...box.querySelectorAll(sel)].filter((el) => !el.hidden && !el.hasAttribute("data-altri"));
	}
	const idOf = (el) => el.dataset.tileId || el.dataset.id;
	function itemFrom(target) {
		if (!target || target.closest("[data-pin]")) return null;
		const title = target.closest(".category > .section-title");
		if (title) return title.parentElement;
		const tile = target.closest(".tile");
		return tile && tile.parentElement.matches("[data-sortable]") ? tile : null;
	}

	// ── Salvataggio: una richiesta alla volta, nell'ordine delle modifiche ──
	let queue = Promise.resolve();
	function post(url, ids) {
		const data = new URLSearchParams();
		ids.forEach((id) => data.append("id", id));
		queue = queue.then(() => fetch(url, {
			method: "POST",
			body: data,
			credentials: "same-origin",
			headers: { "Content-Type": "application/x-www-form-urlencoded" },
		}).then((r) => (r.ok ? r.json() : null)).then((j) => {
			if (!j || j.ok !== true) throw new Error("salvataggio");
		})).catch(fail);
		return queue;
	}
	let failed = false;
	function fail() {
		if (failed) return;
		failed = true;
		say("Non è stato possibile salvare. Ricarico la pagina.");
		window.alert("Non è stato possibile salvare la modifica. La pagina si ricarica con l'ultimo ordine salvato.");
		store((s) => s.setItem(KEY, "1"));
		location.reload();
	}

	function pinnedIds() {
		const g = favGrid();
		return g ? [...g.querySelectorAll(":scope > .tile[data-pinned]")].map(idOf) : [];
	}
	// Preferiti: i fissati nell'ordine in pagina; extra (trascinato o appena
	// fissato) diventa fissato dove si trova, o in fondo se non è in sezione.
	let pendingFav = 0; // salvataggi dei preferiti non ancora ridisegnati
	function saveFavorites(extra, remove) {
		const g = favGrid();
		let ids = g ? [...g.querySelectorAll(":scope > .tile")].filter((t) => t.hasAttribute("data-pinned") || idOf(t) === extra).map(idOf) : [];
		if (extra && !ids.includes(extra)) ids.push(extra);
		if (remove) ids = ids.filter((id) => id !== remove);
		pendingFav++;
		post("/preferiti", ids);
		queue = queue.then(refreshFavorites);
		return queue;
	}
	// Salva il contenitore e annuncia la posizione: nei preferiti dopo il
	// refresh, perché il server rimette i fissati prima degli automatici.
	function save(box, moved) {
		const kind = box.dataset.sortable;
		if (kind === "preferiti") {
			const id = idOf(moved);
			return saveFavorites(id).then(() => announceFav(id));
		}
		const ids = items(box).map(idOf);
		announce(moved, box);
		return post(kind === "categorie" ? "/ordine/categorie" : `/ordine/categorie/${box.dataset.id}`, ids);
	}

	// Dopo un salvataggio dei preferiti: sezione nuova dal server, si resta in modifica.
	function refreshFavorites() {
		pendingFav--;
		if (failed || pendingFav > 0) return; // ne arriva un altro: si ridisegna solo l'ultimo
		return fetch("/partials/preferiti", { credentials: "same-origin" })
			.then((r) => (r.ok ? r.text() : Promise.reject(new Error("parziale"))))
			.then((html) => {
				const old = document.getElementById("preferiti");
				if (!old) return;
				const keep = document.activeElement?.closest?.("#preferiti .tile")?.dataset.tileId;
				const tpl = document.createElement("template");
				tpl.innerHTML = html.trim();
				const next = tpl.content.querySelector("#preferiti");
				if (!next) return;
				old.replaceWith(next);
				sync();
				if (keep) next.querySelector(`[data-tile-id="${keep}"] .tile-main`)?.focus();
			}).catch(fail);
	}

	// Stato delle puntine e dei pulsanti dopo ogni cambio.
	function sync() {
		const sec = document.getElementById("preferiti");
		const pinned = new Set(pinnedIds());
		const full = pinned.size >= Number(sec?.dataset.max); // favMax del server
		document.querySelectorAll("[data-pin]").forEach((b) => {
			const on = pinned.has(b.dataset.pin);
			const fav = !!b.closest('[data-sortable="preferiti"]');
			b.setAttribute("aria-pressed", on ? "true" : "false");
			b.disabled = !on && full;
			b.title = on ? "Sblocca" : full ? "Sblocca prima un preferito" : fav ? "Fissa" : "Aggiungi ai preferiti";
		});
		if (!sec) return;
		const on = editing();
		const toggle = sec.querySelector("[data-personalizza-toggle]");
		if (toggle) {
			toggle.setAttribute("aria-pressed", on ? "true" : "false");
			toggle.querySelector("[data-label]").textContent = on ? "Fatto" : "Personalizza";
			toggle.querySelector(".material-icons").textContent = on ? "check" : "tune";
		}
		const reset = sec.querySelector("[data-personalizza-reset]");
		if (reset) reset.hidden = !on;
	}

	// ── Entrata e uscita ──
	function enter() {
		body.classList.add("personalizza");
		const open = document.querySelector(".tile.open");
		if (open) {
			open.classList.remove("open");
			open.style.height = "";
			delete open.dataset.via;
		}
		// Categorie con soli applicativi non destinati: nascoste in modifica.
		document.querySelectorAll(".category[data-category]").forEach((c) => {
			if (!c.querySelector(".tile:not([data-altri])")) { c.hidden = true; c.dataset.persHidden = ""; }
			c.querySelector(".section-title")?.setAttribute("tabindex", "0");
		});
		sync();
		say("Modalità Personalizza: trascina o usa le frecce per spostare.");
	}
	function exit() {
		abortDrag();
		body.classList.remove("personalizza");
		document.querySelectorAll(".category[data-category]").forEach((c) => {
			if ("persHidden" in c.dataset) { c.hidden = false; delete c.dataset.persHidden; }
			c.querySelector(".section-title")?.removeAttribute("tabindex");
		});
		sync();
	}

	// In modifica i clic non aprono nulla (né app, né guide, né badge): in
	// cattura su window, prima di dashboard.js e del beacon delle aperture.
	const block = (e) => {
		if (!editing()) return;
		const t = e.target;
		if (t.closest?.("[data-personalizza-toggle], [data-personalizza-reset]")) return;
		if (!t.closest?.(".apps, #preferiti")) return;
		e.preventDefault();
		e.stopPropagation();
		const pin = t.closest("[data-pin]");
		if (e.type === "click" && pin && !pin.disabled) {
			const id = pin.dataset.pin;
			if (pin.getAttribute("aria-pressed") === "true") saveFavorites(null, id);
			else saveFavorites(id);
		}
	};
	window.addEventListener("click", block, true);
	window.addEventListener("auxclick", block, true);
	window.addEventListener("dragstart", (e) => { if (editing()) e.preventDefault(); }, true);
	window.addEventListener("contextmenu", (e) => { if (editing() && itemFrom(e.target)) e.preventDefault(); }, true);

	document.addEventListener("click", (e) => {
		if (e.target.closest("[data-personalizza-toggle]")) {
			if (editing()) exit(); else enter();
		} else if (e.target.closest("[data-personalizza-reset]")) {
			if (!window.confirm("Togliere i preferiti fissati e tornare all'ordine iniziale degli applicativi?")) return;
			post("/ordine/ripristina", []).then(() => {
				if (failed) return;
				store((s) => s.setItem(KEY, "1"));
				location.reload();
			});
		}
	});

	// ── Trascinamento ──
	let drag = null;
	function onDown(e) {
		if (!editing() || drag || (e.pointerType === "mouse" && e.button !== 0)) return;
		const el = itemFrom(e.target);
		if (!el) return;
		drag = { el, box: el.parentElement, id: e.pointerId, x0: e.clientX, y0: e.clientY, active: false, timer: null };
		if (pendingFav > 0 && el.closest("#preferiti")) { drag = null; return; } // sezione in aggiornamento
		if (e.pointerType === "mouse") {
			e.preventDefault(); // niente selezione del testo, ma il focus sì (poi le frecce)
			(el.matches(".category") ? el.querySelector(".section-title") : el.querySelector(".tile-main"))?.focus({ preventScroll: true });
		} else drag.timer = setTimeout(() => start(e.clientX, e.clientY), LONG_PRESS_MS);
	}
	function start(x, y) {
		const d = drag;
		if (!d || d.active) return;
		d.active = true;
		d.from = items(d.box).indexOf(d.el);
		const r = d.el.getBoundingClientRect();
		d.dx = x - r.left;
		d.dy = y - r.top;
		d.ph = document.createElement("div");
		d.ph.className = "drag-placeholder" + (d.el.classList.contains("category") ? " is-category" : "");
		d.ph.style.height = `${r.height}px`;
		d.el.after(d.ph);
		d.el.classList.add("dragging");
		Object.assign(d.el.style, { position: "fixed", left: `${r.left}px`, top: `${r.top}px`, width: `${r.width}px`, height: `${r.height}px`, zIndex: "1000" });
		try { d.el.setPointerCapture(d.id); } catch (_) { /* puntatore già rilasciato */ }
	}
	function onMove(e) {
		const d = drag;
		if (!d || e.pointerId !== d.id) return;
		if (!d.active) {
			const far = Math.hypot(e.clientX - d.x0, e.clientY - d.y0);
			if (e.pointerType === "mouse") { if (far > MOUSE_PX) start(e.clientX, e.clientY); }
			else if (far > SCROLL_PX) cancel(); // sta scorrendo la pagina
			if (!d.active) return;
		}
		d.el.style.left = `${e.clientX - d.dx}px`;
		d.el.style.top = `${e.clientY - d.dy}px`;
		// Categorie: bersaglio è tutta la categoria (titolo e riquadri), non solo il titolo.
		const hit = document.elementFromPoint(e.clientX, e.clientY);
		const over = d.box.dataset.sortable === "categorie" ? hit?.closest(".category") : itemFrom(hit);
		if (!over || over === d.el || over.parentElement !== d.box) return;
		const r = over.getBoundingClientRect();
		const before = d.box.dataset.sortable === "categorie" ? e.clientY < r.top + r.height / 2 : e.clientX < r.left + r.width / 2;
		if (before) over.before(d.ph); else over.after(d.ph);
	}
	function finish(e) {
		const d = drag;
		if (!d || e.pointerId !== d.id) return;
		clearTimeout(d.timer);
		drag = null;
		if (!d.active) return;
		d.ph.replaceWith(d.el);
		d.el.classList.remove("dragging");
		d.el.removeAttribute("style");
		if (d.el.matches(".tile")) d.el.style.background = d.bg;
		// Rilasciato al suo posto: niente salvataggio (l'ordine admin resta,
		// un automatico non diventa fissato).
		if (items(d.box).indexOf(d.el) === d.from) return;
		save(d.box, d.el);
	}
	function cancel() {
		if (!drag) return;
		clearTimeout(drag.timer);
		drag = null;
	}
	// Trascinamento annullato: il riquadro non si è mai mosso nel DOM (era solo
	// fixed sopra la pagina), basta togliere segnaposto e stili.
	function abortDrag() {
		const d = drag;
		cancel();
		if (!d || !d.active) return false;
		d.ph.remove();
		d.el.classList.remove("dragging");
		d.el.removeAttribute("style");
		if (d.el.matches(".tile")) d.el.style.background = d.bg;
		return true;
	}
	document.addEventListener("pointerdown", (e) => {
		onDown(e);
		if (drag) drag.bg = drag.el.style.background; // colore pastello inline della tile
	});
	document.addEventListener("pointermove", onMove);
	document.addEventListener("pointerup", finish);
	document.addEventListener("pointercancel", (e) => {
		if (drag && drag.active) finish(e); else cancel();
	});
	// Mentre si trascina col dito la pagina non scorre.
	document.addEventListener("touchmove", (e) => { if (drag && drag.active) e.preventDefault(); }, { passive: false });

	function announce(el, box) {
		const list = items(box);
		say(`Spostato in posizione ${list.indexOf(el) + 1} di ${list.length}`);
	}
	function announceFav(id) {
		const g = favGrid();
		const el = g?.querySelector(`:scope > .tile[data-tile-id="${id}"]`);
		if (el) announce(el, g);
	}

	// ── Tastiera: frecce = posizione precedente o successiva; Esc esce ──
	document.addEventListener("keydown", (e) => {
		if (!editing()) return;
		if (e.key === "Escape") {
			if (abortDrag()) return; // prima annulla il trascinamento
			if (e.target.closest("input, textarea, select, dialog") || document.querySelector("dialog[open]")) return;
			exit();
			return;
		}
		const back = e.key === "ArrowLeft" || e.key === "ArrowUp";
		if (!back && e.key !== "ArrowRight" && e.key !== "ArrowDown") return;
		const el = itemFrom(document.activeElement);
		if (!el) return;
		e.preventDefault();
		const box = el.parentElement;
		let list = items(box);
		// Un fissato si sposta solo tra i fissati: dopo gli automatici tornerebbe indietro.
		if (box === favGrid() && el.hasAttribute("data-pinned")) list = list.filter((t) => t.hasAttribute("data-pinned"));
		const i = list.indexOf(el);
		const j = back ? i - 1 : i + 1;
		if (i < 0 || j < 0 || j >= list.length) return;
		const focus = document.activeElement;
		if (back) list[j].before(el); else list[j].after(el);
		focus.focus();
		save(box, el);
	});

	// Dopo una ricarica per errore si torna in modifica (solo quella volta).
	if (store((s) => s.getItem(KEY))) {
		store((s) => s.removeItem(KEY));
		if (document.getElementById("preferiti")) enter();
	}
	sync();
})();
