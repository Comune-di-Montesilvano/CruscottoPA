// Plancia: orologio e saluto, ricerca, carosello avvisi, urgenti a tutto
// schermo, schede delle tile, popover del calendario, fallback delle icone.
// Nessun handler inline: la CSP consente solo script da 'self'.
(() => {
	"use strict";

	const DAYS = ["domenica", "lunedì", "martedì", "mercoledì", "giovedì", "venerdì", "sabato"];
	const MONTHS = ["gennaio", "febbraio", "marzo", "aprile", "maggio", "giugno",
		"luglio", "agosto", "settembre", "ottobre", "novembre", "dicembre"];

	// Stesse soglie di greeting() in internal/web/dashboard.go.
	const greeting = (h) => (h >= 6 && h < 13) ? "Buongiorno" : (h >= 13 && h < 18) ? "Buon pomeriggio" : "Buonasera";

	function tick() {
		const now = new Date();
		const set = (sel, text) => { const el = document.querySelector(sel); if (el) el.textContent = text; };
		set("[data-clock]", now.toLocaleTimeString("it-IT", { hour: "2-digit", minute: "2-digit" }));
		set("[data-greeting]", greeting(now.getHours()));
		set("[data-date]", `${DAYS[now.getDay()]} ${now.getDate()} ${MONTHS[now.getMonth()]} ${now.getFullYear()}`);
	}
	tick();
	setInterval(tick, 15000);

	// ── Ricerca ──────────────────────────────────────────
	const input = document.getElementById("search");
	const norm = (s) => s.normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();

	function filter() {
		const q = norm(input.value.trim());
		let any = false;
		document.querySelectorAll("[data-search-item]").forEach((el) => {
			const hit = q === "" || norm(el.dataset.search || "").includes(q);
			el.hidden = !hit;
			any = any || hit;
		});
		document.querySelectorAll("[data-category]").forEach((c) => {
			c.hidden = !c.querySelector("[data-search-item]:not([hidden])");
		});
		const guides = document.querySelector(".widget.guides");
		if (guides) guides.hidden = !guides.querySelector("[data-search-item]:not([hidden])");
		const none = document.getElementById("no-results");
		if (none) none.hidden = any || document.querySelectorAll("[data-search-item]").length === 0;
	}
	if (input) input.addEventListener("input", filter);

	// ── Carosello avvisi ─────────────────────────────────
	const ROTATE_MS = 12000;
	const reduceMotion = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
	let carousel = null;

	function initCarousel() {
		const keepId = carousel ? carousel.currentId : null;
		if (carousel) clearInterval(carousel.timer);
		carousel = null;
		const root = document.querySelector("[data-carousel]");
		if (!root) return;
		const cards = Array.from(root.querySelectorAll(".news"));
		const dots = Array.from(root.querySelectorAll(".dot"));
		const counter = root.querySelector("[data-counter]");
		const pauseBtn = root.querySelector("[data-pause]");
		const state = { index: 0, paused: false, hover: false, timer: null, currentId: null };
		root.classList.add("js-on");

		function show(i) {
			state.index = (i + cards.length) % cards.length;
			cards.forEach((c, k) => c.classList.toggle("active", k === state.index));
			dots.forEach((d, k) => d.classList.toggle("on", k === state.index));
			if (counter) counter.textContent = `${state.index + 1} di ${cards.length}`;
			state.currentId = cards[state.index].dataset.alert;
		}

		// Dopo un aggiornamento HTMX si riparte dall'avviso che era visibile.
		show(Math.max(0, cards.findIndex((c) => c.dataset.alert === keepId)));

		if (cards.length > 1 && !reduceMotion) {
			state.timer = setInterval(() => {
				if (!state.paused && !state.hover) show(state.index + 1);
			}, ROTATE_MS);
		} else if (pauseBtn) {
			pauseBtn.hidden = true;
		}
		root.querySelector("[data-prev]")?.addEventListener("click", () => show(state.index - 1));
		root.querySelector("[data-next]")?.addEventListener("click", () => show(state.index + 1));
		pauseBtn?.addEventListener("click", () => {
			state.paused = !state.paused;
			pauseBtn.textContent = state.paused ? "▶" : "❚❚";
			pauseBtn.setAttribute("aria-label", state.paused ? "Riprendi lo scorrimento" : "Metti in pausa");
		});
		root.addEventListener("mouseenter", () => { state.hover = true; });
		root.addEventListener("mouseleave", () => { state.hover = false; });
		root.addEventListener("focusin", () => { state.hover = true; });
		root.addEventListener("focusout", () => { state.hover = false; });
		root.addEventListener("keydown", (e) => {
			if (e.key === "ArrowLeft") show(state.index - 1);
			else if (e.key === "ArrowRight") show(state.index + 1);
		});
		carousel = state;
	}

	// ── Urgenti a tutto schermo (una volta per avviso e versione) ──
	let urgentOpen = false;
	const readKey = (d) => `cruscotto:letto:${d.dataset.urgent}:${d.dataset.version}`;
	function isRead(d) {
		try { return localStorage.getItem(readKey(d)) === "1"; } catch (_) { return false; }
	}
	function markRead(d) {
		try { localStorage.setItem(readKey(d), "1"); } catch (_) { /* navigazione privata o criteri: si ripresenterà */ }
	}
	function showUrgents() {
		if (urgentOpen) return;
		const next = Array.from(document.querySelectorAll("dialog.urgent")).find((d) => !d.dataset.shown && !isRead(d));
		if (!next) return;
		next.dataset.shown = "1";
		urgentOpen = true;
		next.addEventListener("cancel", (e) => e.preventDefault()); // si chiude solo con "Ho letto"
		next.addEventListener("close", () => {
			markRead(next);
			urgentOpen = false;
			showUrgents();
		}, { once: true });
		next.showModal();
	}

	// Il refresh degli avvisi non deve far sparire un urgente aperto.
	document.addEventListener("htmx:beforeSwap", (e) => {
		if (e.detail.target.id === "alerts" && urgentOpen) e.detail.shouldSwap = false;
	});
	document.addEventListener("htmx:afterSwap", (e) => {
		if (e.detail.target.id === "alerts") {
			initCarousel();
			showUrgents();
		}
	});

	// ── Schede delle tile (badge su touch, Esc, click fuori) ──
	function closeTile(tile) {
		tile.classList.remove("open");
		tile.querySelector("[data-flyout-toggle]")?.setAttribute("aria-expanded", "false");
	}
	document.addEventListener("click", (e) => {
		const open = document.querySelector(".tile.open");
		const badge = e.target.closest("[data-flyout-toggle]");
		if (badge) {
			const tile = badge.closest(".tile");
			const willOpen = !tile.classList.contains("open");
			if (open && open !== tile) closeTile(open);
			tile.classList.toggle("open", willOpen);
			badge.setAttribute("aria-expanded", String(willOpen));
			return;
		}
		if (open && !e.target.closest(".tile.open")) closeTile(open);
	});

	// ── Tastiera: "/" ricerca, Esc chiude ricerca e schede ──
	document.addEventListener("keydown", (e) => {
		const typing = e.target.closest("input, textarea, select, [contenteditable]");
		if (e.key === "/" && !typing && input) {
			e.preventDefault();
			input.focus();
		} else if (e.key === "Escape") {
			if (input && e.target === input) {
				input.value = "";
				filter();
				input.blur();
			}
			const open = document.querySelector(".tile.open");
			if (open) closeTile(open);
			if (document.activeElement && document.activeElement.closest(".tile")) document.activeElement.blur();
		}
	});

	// ── Popover dei giorni del calendario: sotto il giorno cliccato ──
	document.addEventListener("toggle", (e) => {
		const pop = e.target;
		if (!(pop instanceof HTMLElement) || !pop.matches(".pop") || e.newState !== "open") return;
		const btn = document.querySelector(`[popovertarget="${pop.id}"]`);
		if (!btn) return;
		const r = btn.getBoundingClientRect();
		const left = Math.max(8, Math.min(r.left + r.width / 2 - pop.offsetWidth / 2, window.innerWidth - pop.offsetWidth - 8));
		const below = r.bottom + 6;
		const top = below + pop.offsetHeight > window.innerHeight - 8 ? Math.max(8, r.top - pop.offsetHeight - 6) : below;
		pop.style.left = `${left}px`;
		pop.style.top = `${top}px`;
	}, true);

	// ── Icona da URL non raggiungibile → iniziali ──
	document.addEventListener("error", (e) => {
		const img = e.target;
		if (!(img instanceof HTMLImageElement) || !img.dataset.fallback) return;
		const box = img.closest(".tile-icon");
		if (box) {
			box.textContent = img.dataset.fallback;
			box.classList.add("tile-mono");
			box.style.color = img.dataset.color || "";
			return;
		}
		const span = document.createElement("span");
		span.className = "app-icon";
		span.textContent = img.dataset.fallback;
		span.style.background = img.dataset.color || "";
		img.replaceWith(span);
	}, true);

	initCarousel();
	showUrgents();
})();
