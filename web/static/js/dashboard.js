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

	// ── Ricerca a tendina: indice JSON scritto dal server (già filtrato) ──
	const input = document.getElementById("search");
	const list = document.getElementById("search-results");
	const norm = (s) => (s || "").normalize("NFD").replace(/[\u0300-\u036f]/g, "").toLowerCase();
	let index = [];
	try { index = JSON.parse(document.getElementById("search-index")?.textContent || "[]") || []; } catch (_) { index = []; }
	index.forEach((it) => { it.nt = norm(it.t); it.nx = norm(`${it.s || ""} ${it.x || ""}`); });
	const GROUPS = [["app", "Applicativi", "apps"], ["guide", "Guide", "menu_book"], ["support", "Assistenza", "support_agent"]];
	const MAX_PER_GROUP = 8;
	let options = [];
	let active = -1;

	// 0 inizio del titolo, 1 inizio di una parola del titolo, 2 dentro il
	// titolo, 3 nel resto (categoria, descrizione, app coperte); -1 nessuno.
	function score(it, q) {
		if (it.nt.startsWith(q)) return 0;
		if (it.nt.split(/\s+/).some((w) => w.startsWith(q))) return 1;
		if (it.nt.includes(q)) return 2;
		if (it.nx.includes(q)) return 3;
		return -1;
	}
	function el(tag, cls, text) {
		const e = document.createElement(tag);
		if (cls) e.className = cls;
		if (text) e.textContent = text;
		return e;
	}
	function closeResults() {
		if (!list) return;
		list.hidden = true;
		list.replaceChildren();
		input.setAttribute("aria-expanded", "false");
		input.removeAttribute("aria-activedescendant");
		options = [];
		active = -1;
	}
	// scroll: solo coi tasti; col mouse la lista non deve muoversi sotto il puntatore.
	function setActive(i, scroll) {
		options.forEach((o, k) => {
			o.classList.toggle("active", k === i);
			o.setAttribute("aria-selected", String(k === i));
		});
		active = i;
		if (i >= 0) {
			input.setAttribute("aria-activedescendant", options[i].id);
			if (scroll) options[i].scrollIntoView({ block: "nearest" });
		} else {
			input.removeAttribute("aria-activedescendant");
		}
	}
	function render() {
		const q = norm(input.value.trim());
		if (!q || !list) { closeResults(); return; }
		list.replaceChildren();
		options = [];
		GROUPS.forEach(([kind, label, icon]) => {
			const hits = index.map((it) => [score(it, q), it]).filter(([sc, it]) => sc >= 0 && it.k === kind)
				.sort((a, b) => a[0] - b[0] || a[1].t.localeCompare(b[1].t, "it")).slice(0, MAX_PER_GROUP);
			if (!hits.length) return;
			const group = el("div", "search-group-box");
			group.setAttribute("role", "group");
			const head = el("div", "search-group", label);
			head.id = `search-group-${kind}`;
			group.setAttribute("aria-labelledby", head.id);
			group.append(head);
			list.append(group);
			hits.forEach(([, it]) => {
				const a = el("a", "search-option");
				a.id = `search-opt-${options.length}`;
				a.href = it.u;
				a.setAttribute("role", "option");
				a.tabIndex = -1; // il focus resta sull'input (combobox)
				if (it.n) { a.target = "_blank"; a.rel = "noopener"; }
				const ic = el("span", "material-icons", it.k === "app" && it.i ? it.i : icon);
				ic.setAttribute("aria-hidden", "true");
				if (it.k === "app" && it.c) ic.style.color = it.c;
				const txt = el("span", "search-text");
				txt.append(el("span", "search-title", it.t));
				if (it.s) txt.append(el("span", "search-sub", it.k === "support" ? `Assistenza per ${it.s}` : it.s));
				a.append(ic, txt);
				a.addEventListener("mousemove", () => setActive(options.indexOf(a), false));
				options.push(a);
				group.append(a);
			});
		});
		if (!options.length) list.append(el("div", "search-empty", `Nessun risultato per «${input.value.trim()}»`));
		list.hidden = false;
		input.setAttribute("aria-expanded", "true");
		setActive(options.length ? 0 : -1, true);
	}
	if (input && list) {
		input.addEventListener("input", render);
		input.addEventListener("focus", () => { if (input.value.trim()) render(); });
		input.addEventListener("keydown", (e) => {
			if (list.hidden) return;
			if (e.key === "ArrowDown" && options.length) { e.preventDefault(); setActive((active + 1) % options.length, true); }
			else if (e.key === "ArrowUp" && options.length) { e.preventDefault(); setActive((active - 1 + options.length) % options.length, true); }
			else if (e.key === "Enter" && active >= 0) { e.preventDefault(); options[active].click(); }
		});
		document.addEventListener("click", (e) => { if (!e.target.closest(".search-wrap")) closeResults(); });
		// Uscendo dalla ricerca (Tab, clic altrove) la tendina si chiude.
		const wrap = input.closest(".search-wrap");
		wrap?.addEventListener("focusout", (e) => { if (!wrap.contains(e.relatedTarget)) closeResults(); });
	}

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
				closeResults();
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
