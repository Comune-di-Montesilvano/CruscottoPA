// Plancia: orologio/saluto, ricerca, dialog avvisi, popover guide, fallback icone.
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

	// ── Ricerca ──────────────────────────────────────────────
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
		const guides = document.querySelector("aside.guides");
		if (guides) guides.hidden = !guides.querySelector("[data-search-item]:not([hidden])");
		const none = document.getElementById("no-results");
		if (none) none.hidden = any || document.querySelectorAll("[data-search-item]").length === 0;
	}

	if (input) {
		input.addEventListener("input", filter);
		document.addEventListener("keydown", (e) => {
			const typing = e.target.closest("input, textarea, select, [contenteditable]");
			if (e.key === "/" && !typing) {
				e.preventDefault();
				input.focus();
			} else if (e.key === "Escape" && e.target === input) {
				input.value = "";
				filter();
				input.blur();
			}
		});
	}

	// ── Dialog avvisi (delega: la striscia è ricaricata da HTMX) ──
	document.addEventListener("click", (e) => {
		const btn = e.target.closest("[data-dialog]");
		if (btn) document.getElementById(btn.dataset.dialog)?.showModal();
	});

	// ── Posizione popover guide sotto il segmento "?" ──
	document.addEventListener("toggle", (e) => {
		const pop = e.target;
		if (!(pop instanceof HTMLElement) || !pop.matches(".guides-pop") || e.newState !== "open") return;
		const btn = document.querySelector(`[popovertarget="${pop.id}"]`);
		if (!btn) return;
		const r = btn.getBoundingClientRect();
		const left = Math.max(8, Math.min(r.right - pop.offsetWidth, window.innerWidth - pop.offsetWidth - 8));
		const below = r.bottom + 6;
		const top = below + pop.offsetHeight > window.innerHeight - 8 ? Math.max(8, r.top - pop.offsetHeight - 6) : below;
		pop.style.left = `${left}px`;
		pop.style.top = `${top}px`;
	}, true);

	// ── Icona da URL esterno non raggiungibile → monogramma ──
	document.addEventListener("error", (e) => {
		const img = e.target;
		if (!(img instanceof HTMLImageElement) || !img.dataset.fallback) return;
		const span = document.createElement("span");
		span.className = "app-icon";
		span.textContent = img.dataset.fallback;
		span.style.background = img.dataset.color || "";
		img.replaceWith(span);
	}, true);
})();
