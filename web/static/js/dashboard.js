// Plancia: orologio e saluto, ricerca, carosello avvisi, urgenti a tutto
// schermo, schede delle tile, popover del calendario, fallback delle icone.
// Nessun handler inline: la CSP consente solo script da 'self'.
(() => {
	"use strict";

	// Stesse soglie di greeting() in internal/web/dashboard.go.
	const greeting = (h) => (h >= 6 && h < 13) ? "Buongiorno" : (h >= 13 && h < 18) ? "Buon pomeriggio" : "Buonasera";

	function tick() {
		const now = new Date();
		const set = (sel, text) => { const el = document.querySelector(sel); if (el) el.textContent = text; };
		set("[data-clock]", now.toLocaleTimeString("it-IT", { hour: "2-digit", minute: "2-digit" }));
		set("[data-greeting]", greeting(now.getHours()));
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
	const NEWS_HOVER_MS = 500;
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
		const state = { index: 0, paused: false, hover: false, expanded: false, timer: null, currentId: null };
		root.classList.add("js-on");

		function show(i) {
			// Cambiando avviso si richiude quello espanso.
			root.querySelectorAll(".news-expand[open]").forEach((d) => { d.open = false; });
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
				if (!state.paused && !state.hover && !state.expanded) show(state.index + 1);
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
		// Un avviso espanso copre la pagina invece di spingerla (il carosello
		// tiene l'altezza di prima) e ferma lo scorrimento finché non si riduce.
		const track = root.querySelector(".carousel-track");
		let newsTimer = null;
		function openNews(more, via) {
			clearTimeout(newsTimer);
			if (!more.open) track.style.height = `${track.offsetHeight}px`;
			more.dataset.via = via;
			more.open = true;
		}
		root.addEventListener("click", (e) => {
			const summary = e.target.closest(".news-expand > summary");
			if (summary) {
				// Aperto dal mouse: il clic lo fissa invece di chiuderlo.
				const more = summary.parentElement;
				e.preventDefault();
				if (more.open && more.dataset.via !== "hover") more.open = false;
				else openNews(more, "click");
				return;
			}
			// Clic sull'estratto: come "Continua a leggere" (non sui link).
			const more = e.target.closest(".news-short")?.parentElement.querySelector(":scope > .news-expand");
			if (more && !e.target.closest("a")) openNews(more, "click");
		}, true);
		// Col mouse sopra per mezzo secondo l'avviso lungo si apre; uscendo si
		// richiude, se non è stato fissato col clic.
		root.addEventListener("pointerover", (e) => {
			if (e.pointerType !== "mouse") return;
			const card = e.target.closest(".news.active");
			const more = card?.querySelector(":scope > .news-expand");
			if (!more || more.open || card.contains(e.relatedTarget)) return;
			clearTimeout(newsTimer);
			newsTimer = setTimeout(() => { if (!more.open) openNews(more, "hover"); }, NEWS_HOVER_MS);
		});
		root.addEventListener("pointerout", (e) => {
			if (e.pointerType !== "mouse") return;
			const card = e.target.closest(".news");
			if (!card || card.contains(e.relatedTarget)) return;
			clearTimeout(newsTimer);
			const more = card.querySelector(":scope > .news-expand[open]");
			if (more && more.dataset.via === "hover") more.open = false;
		});
		root.addEventListener("toggle", () => {
			state.expanded = !!root.querySelector(".news-expand[open]");
			if (!state.expanded) track.style.height = "";
			root.querySelectorAll(".news-expand:not([open])").forEach((d) => { delete d.dataset.via; });
		}, true);
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
	// Copia in memoria: senza localStorage (navigazione privata, criteri) un urgente
	// letto non si riapre al refresh HTMX ogni 5 minuti, solo al ricaricamento.
	const readInPage = new Set();
	function isRead(d) {
		if (readInPage.has(readKey(d))) return true;
		try { return localStorage.getItem(readKey(d)) === "1"; } catch (_) { return false; }
	}
	function markRead(d) {
		readInPage.add(readKey(d));
		try { localStorage.setItem(readKey(d), "1"); } catch (_) { /* si ripresenterà al prossimo caricamento */ }
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

	// Il refresh degli avvisi non deve far sparire un urgente aperto né
	// richiudere un avviso che si sta leggendo.
	document.addEventListener("htmx:beforeSwap", (e) => {
		if (e.detail.target.id !== "alerts") return;
		if (urgentOpen || e.detail.target.querySelector(".news-expand[open]")) e.detail.shouldSwap = false;
	});
	document.addEventListener("htmx:afterSwap", (e) => {
		if (e.detail.target.id === "alerts") {
			initCarousel();
			showUrgents();
		}
	});

	// ── Tile: si allunga dopo 300 ms col mouse sopra, col focus o col badge
	// (touch). Aperta dal badge resta aperta finché non si chiude (Esc, click fuori).
	const HOVER_OPEN_MS = 300;
	let hoverTimer = null;
	function closeTile(tile) {
		tile.classList.remove("open");
		tile.style.height = "";
		delete tile.dataset.via;
		tile.querySelector("[data-tile-toggle]")?.setAttribute("aria-expanded", "false");
	}
	function openTile(tile, via) {
		clearTimeout(hoverTimer);
		const open = document.querySelector(".tile.open");
		if (open && open !== tile) closeTile(open);
		// La card aperta esce dal flusso: la tile tiene la sua altezza.
		if (!tile.classList.contains("open")) tile.style.height = `${tile.offsetHeight}px`;
		tile.classList.add("open");
		tile.dataset.via = via;
		tile.querySelector("[data-tile-toggle]")?.setAttribute("aria-expanded", "true");
	}
	document.addEventListener("pointerover", (e) => {
		if (e.pointerType !== "mouse") return;
		const tile = e.target.closest(".tile");
		if (!tile || tile.contains(e.relatedTarget) || tile.classList.contains("open")) return;
		clearTimeout(hoverTimer);
		hoverTimer = setTimeout(() => { if (!tile.classList.contains("open")) openTile(tile, "hover"); }, HOVER_OPEN_MS);
	});
	document.addEventListener("pointerout", (e) => {
		if (e.pointerType !== "mouse") return;
		const tile = e.target.closest(".tile");
		if (!tile || tile.contains(e.relatedTarget)) return;
		clearTimeout(hoverTimer);
		if (tile.dataset.via === "hover") closeTile(tile);
	});
	document.addEventListener("focusin", (e) => {
		const tile = e.target.closest(".tile");
		if (tile && !tile.classList.contains("open")) openTile(tile, "focus");
	});
	document.addEventListener("focusout", (e) => {
		const tile = e.target.closest(".tile");
		if (tile && !tile.contains(e.relatedTarget) && tile.dataset.via === "focus") closeTile(tile);
	});
	document.addEventListener("click", (e) => {
		const open = document.querySelector(".tile.open");
		const badge = e.target.closest("[data-tile-toggle]");
		if (badge) {
			const tile = badge.closest(".tile");
			// Aperta dal mouse o dal focus: il click la fissa; fissata: la chiude.
			if (tile.dataset.via === "click") closeTile(tile);
			else openTile(tile, "click");
			return;
		}
		if (open && !e.target.closest(".tile.open")) closeTile(open);
		// Click fuori da un avviso espanso (sull'ombra): si riduce.
		const news = document.querySelector(".news-expand[open]");
		if (news && !e.target.closest(".news")) news.open = false;
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
			const news = document.querySelector(".news-expand[open]");
			if (news) news.open = false;
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

	// Riconoscimento (identità dichiarata, solo per personalizzare): una chiamata
	// in background; se il server riconosce l'utente ricarica la pagina una volta.
	// Al massimo un tentativo ogni 24 ore, ricordato nel browser: in produzione
	// il reverse proxy toglie Set-Cookie dai 401, quindi il cookie anonimo del
	// server non arriva ai PC fuori dominio. Anche con i cookie bloccati la
	// ricarica non si ripete. Senza localStorage nessun tentativo.
	const IO_KEY = "cruscotto-io-tentato";
	let firstTry = false;
	try {
		const last = Number(localStorage.getItem(IO_KEY)) || 0;
		firstTry = Date.now() - last > 24 * 60 * 60 * 1000;
		if (firstTry) localStorage.setItem(IO_KEY, String(Date.now()));
	} catch (_) { firstTry = false; }
	// Firefox manda NTLM solo ai siti autorizzati in about:config: a chi resta
	// anonimo spieghiamo come farlo ("Riprova" rifà subito il tentativo).
	const FF_KEY = "cruscotto-firefox-aiuto-chiuso";
	function firefoxHelp() {
		const box = document.querySelector("[data-ff-help]");
		if (!box || !/Firefox\//.test(navigator.userAgent)) return;
		try { if (localStorage.getItem(FF_KEY)) return; } catch (_) { /* lo mostriamo */ }
		box.querySelector("[data-ff-host]").textContent = location.hostname;
		const retry = box.querySelector("[data-ff-retry]");
		retry.addEventListener("click", () => {
			retry.disabled = true;
			tryIo().then((ok) => {
				if (ok) { location.reload(); return; }
				box.querySelector("[data-ff-status]").hidden = false;
				retry.disabled = false;
			});
		});
		box.querySelector("[data-ff-dismiss]").addEventListener("click", () => {
			try { localStorage.setItem(FF_KEY, "1"); } catch (_) { /* tornerà al prossimo accesso */ }
			box.hidden = true;
		});
		box.hidden = false;
	}
	// tryIo: true se il server ha riconosciuto l'utente.
	function tryIo() {
		return fetch("/io", { credentials: "same-origin" })
			.then((r) => (r.ok ? r.json() : null))
			.then((j) => !!(j && j.riconosciuto))
			.catch(() => false);
	}
	if (firstTry && document.body.hasAttribute("data-riconosci")) {
		tryIo().then((ok) => { if (ok) location.reload(); else firefoxHelp(); });
	} else if (document.body.hasAttribute("data-anonimo")) {
		firefoxHelp();
	}

	// "Mostra tutto": preferenza in un cookie letto dal server, poi ricarica.
	const audienceToggle = document.querySelector("[data-mostra-tutto]");
	if (audienceToggle) {
		audienceToggle.hidden = false;
		audienceToggle.addEventListener("click", () => {
			document.cookie = audienceToggle.dataset.mostraTutto === "1"
				? "cruscotto_tutto=1; Path=/; Max-Age=31536000; SameSite=Lax"
				: "cruscotto_tutto=; Path=/; Max-Age=0; SameSite=Lax";
			location.reload();
		});
	}

	initCarousel();
	showUrgents();
})();
