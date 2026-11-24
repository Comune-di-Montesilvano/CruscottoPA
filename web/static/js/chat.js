// Chat informale tra colleghi. Il pannello ([data-chat-panel]) sta nel dialog
// della plancia o nella pagina /chat. Il testo dei messaggi entra nel DOM solo
// come testo (textContent); i link solo http/https. Gli errori mostrano un
// messaggio fisso per codice, mai il corpo della risposta.
(function () {
	"use strict";

	const MAX = 2000;
	const ERRORS = {
		anonimo: "La chat non è disponibile per il tuo utente.",
		ad: "Active Directory non risponde: riprova tra poco.",
		destinatario: "Non puoi scrivere a questo collega.",
		vuoto: "Scrivi un messaggio.",
		lungo: "Il messaggio supera i 2000 caratteri.",
		limite: "Hai scritto troppi messaggi in poco tempo: aspetta qualche minuto.",
		credenziali: "Password non corretta.",
		bloccato: "Troppi tentativi: riprova tra qualche minuto.",
		accesso: "Accedi per leggere i messaggi.",
		conversazione: "Conversazione non disponibile.",
	};
	const errText = (code) => ERRORS[code] || "Non è stato possibile completare l'operazione. Riprova.";
	const OPEN_KEY = "cruscotto-chat-open";

	const panel = document.querySelector("[data-chat-panel]");
	const dialog = panel ? panel.closest("dialog.chat") : null;

	// Senza pannello (es. pagine secondarie) i pulsanti «Scrivi» portano a /chat.
	document.addEventListener("click", (e) => {
		const t = e.target.closest && e.target.closest("[data-chat-open]");
		if (!t) return;
		e.preventDefault();
		const who = t.getAttribute("data-chat-open") || "";
		if (!panel) {
			location.href = who ? "/chat?a=" + encodeURIComponent(who) : "/chat";
			return;
		}
		openPanel(who);
	});
	if (!panel) return;

	const me = panel.dataset.me;
	const verified = panel.dataset.verified === "1";
	const $ = (sel) => panel.querySelector(sel);
	const body = $("[data-chat-body]");
	const list = $("[data-chat-list]");
	const results = $("[data-chat-results]");
	const search = $("[data-chat-search]");
	const thread = $("[data-chat-thread]");
	const empty = $("[data-chat-empty]");
	const msgs = $("[data-chat-msgs]");
	const more = $("[data-chat-more]");
	const form = $("[data-chat-form]");
	const area = form.querySelector("textarea");
	const count = $("[data-chat-count]");
	const err = $("[data-chat-err]");
	const status = $("[data-chat-status]");
	const blockBtn = $("[data-chat-block]");
	const blockedNote = $("[data-chat-blocked]");

	let current = null; // {id, con, nome, bloccato}
	let oldest = 0;

	function el(tag, cls, text) {
		const n = document.createElement(tag);
		if (cls) n.className = cls;
		if (text !== undefined) n.textContent = text;
		return n;
	}
	function initials(name) {
		const w = (name || "").trim().split(/\s+/).filter(Boolean);
		return ((w[0] || "?")[0] + (w.length > 1 ? w[w.length - 1][0] : "")).toUpperCase();
	}
	function say(text) {
		status.textContent = "";
		setTimeout(() => { status.textContent = text; }, 50);
	}
	async function api(method, url, data) {
		const opts = { method, credentials: "same-origin", headers: {} };
		if (data) {
			opts.body = new URLSearchParams(data);
			opts.headers["Content-Type"] = "application/x-www-form-urlencoded";
		}
		try {
			const r = await fetch(url, opts);
			if (!r.ok) return { ok: false, errore: "rete" };
			return await r.json();
		} catch (_) {
			return { ok: false, errore: "rete" };
		}
	}
	// Il widget si ricarica da solo (hx-trigger "chat-local from:body").
	function refreshWidget() {
		document.body.dispatchEvent(new Event("chat-local"));
	}

	// Testo con i link http/https cliccabili, tutto come nodi di testo.
	function appendText(parent, text) {
		const re = /https?:\/\/[^\s<>"]+/g;
		let last = 0, m;
		while ((m = re.exec(text)) !== null) {
			let href = m[0].replace(/[.,;:!?)\]]+$/, "");
			if (m.index > last) parent.append(document.createTextNode(text.slice(last, m.index)));
			let url = null;
			try { url = new URL(href); } catch (_) { url = null; }
			if (url && (url.protocol === "http:" || url.protocol === "https:")) {
				const a = el("a", "", href);
				a.href = url.href;
				a.target = "_blank";
				a.rel = "noopener noreferrer";
				parent.append(a);
			} else {
				parent.append(document.createTextNode(href));
			}
			last = m.index + href.length;
			re.lastIndex = last;
		}
		if (last < text.length) parent.append(document.createTextNode(text.slice(last)));
	}

	function when(iso) {
		const d = new Date(iso);
		if (isNaN(d)) return "";
		const now = new Date();
		const hm = d.toLocaleTimeString("it-IT", { hour: "2-digit", minute: "2-digit" });
		if (d.toDateString() === now.toDateString()) return hm;
		const y = new Date(now); y.setDate(now.getDate() - 1);
		if (d.toDateString() === y.toDateString()) return "ieri " + hm;
		return d.toLocaleDateString("it-IT", { day: "numeric", month: "short" }) + " " + hm;
	}

	function messageItem(m) {
		const li = el("li", "chat-msg" + (m.mio ? " mine" : ""));
		const bubble = el("div", "chat-bubble");
		appendText(bubble, m.testo);
		li.append(bubble);
		const meta = el("div", "chat-meta");
		if (!m.verificato) {
			const u = el("span", "chat-unverified", "⚠ non verificato");
			u.title = "Scritto prima dell'accesso alla chat: non possiamo garantire che l'abbia scritto davvero lui o lei";
			meta.append(u);
		}
		if (m.quando) meta.append(el("time", "", when(m.quando)));
		if (m.locale) meta.append(el("span", "", "inviato"));
		li.append(meta);
		return li;
	}

	// Mobile: una vista alla volta (elenco o conversazione).
	function show(view) {
		body.classList.toggle("show-thread", view === "thread");
		thread.hidden = view !== "thread";
		empty.hidden = view === "thread";
	}

	async function loadList() {
		if (!verified || !list) return;
		const d = await api("GET", "/chat/conversazioni");
		list.replaceChildren();
		if (!d.ok) {
			list.append(el("li", "chat-list-empty", errText(d.errore)));
			return;
		}
		if (!d.conversazioni.length) {
			list.append(el("li", "chat-list-empty", "Nessuna conversazione negli ultimi 30 giorni."));
			return;
		}
		d.conversazioni.forEach((c) => {
			const li = el("li");
			const b = el("button", "chat-conv" + (c.nuovi ? " is-new" : "") + (current && current.con === c.con ? " is-current" : ""));
			b.type = "button";
			b.addEventListener("click", () => openWith(c.con));
			const av = el("span", "chat-avatar", initials(c.nome));
			av.setAttribute("aria-hidden", "true");
			const txt = el("span", "chat-conv-text");
			txt.append(el("span", "chat-conv-name", c.nome), el("span", "chat-conv-when", when(c.ultimo)));
			b.append(av, txt);
			if (c.nuovi) b.append(el("span", "chat-conv-badge", String(c.nuovi)));
			li.append(b);
			list.append(li);
		});
	}

	function renderThread(d, prepend) {
		if (!prepend) {
			current = { id: d.id, con: d.con, nome: d.nome, bloccato: !!d.bloccato };
			$("[data-chat-with]").textContent = d.nome;
			$("[data-chat-avatar]").textContent = initials(d.nome);
			msgs.replaceChildren();
			oldest = 0;
			if (blockBtn) blockBtn.textContent = current.bloccato ? "Sblocca" : "Blocca";
			blockedNote.hidden = !current.bloccato;
			err.hidden = true;
		}
		const items = d.messaggi.map(messageItem);
		if (prepend) msgs.prepend(...items); else msgs.append(...items);
		if (d.messaggi.length) oldest = d.messaggi[0].id;
		more.hidden = !d.altri;
		show("thread");
		if (!prepend) msgs.lastElementChild && msgs.lastElementChild.scrollIntoView({ block: "end" });
	}

	// Letto solo se l'utente lo vede: scheda in primo piano e pannello aperto.
	async function markRead() {
		if (!verified || !current || !current.id) return;
		if (document.visibilityState !== "visible" || (dialog && !dialog.open)) return;
		await api("POST", "/chat/conversazioni/" + current.id + "/letto");
		loadList();
		refreshWidget();
	}

	async function openWith(user) {
		const d = await api("GET", "/chat/con/" + encodeURIComponent(user));
		if (!d.ok) { say(errText(d.errore)); return; }
		renderThread(d, false);
		area.focus();
		markRead();
		loadList();
	}

	async function openConv(id) {
		const d = await api("GET", "/chat/conversazioni/" + id);
		if (!d.ok) { say(errText(d.errore)); return; }
		renderThread(d, false);
		markRead();
	}

	async function reloadCurrent() {
		if (!current || !verified) return;
		const d = await api("GET", "/chat/con/" + encodeURIComponent(current.con));
		if (d.ok) { renderThread(d, false); markRead(); }
	}

	function openPanel(who) {
		if (dialog && !dialog.open) dialog.showModal();
		loadList();
		if (who) openWith(who);
		else if (!current) { show("list"); search.focus(); }
	}

	// Ricerca dei colleghi (nome e cognome in AD).
	let searchTimer = 0, searchCtl = null;
	search.addEventListener("input", () => {
		clearTimeout(searchTimer);
		const q = search.value.trim();
		if (q.length < 2) { results.hidden = true; results.replaceChildren(); return; }
		searchTimer = setTimeout(async () => {
			if (searchCtl) searchCtl.abort();
			searchCtl = new AbortController();
			let d;
			try {
				const r = await fetch("/chat/persone?q=" + encodeURIComponent(q), { credentials: "same-origin", signal: searchCtl.signal });
				d = await r.json();
			} catch (_) { return; }
			if (search.value.trim() !== q) return;
			results.replaceChildren();
			if (!d.ok || !d.persone.length) {
				results.append(el("li", "chat-list-empty", d.ok ? "Nessun collega trovato." : errText(d.errore)));
			} else {
				d.persone.forEach((p) => {
					const li = el("li");
					const b = el("button", "chat-conv");
					b.type = "button";
					const av = el("span", "chat-avatar", initials(p.nome));
					av.setAttribute("aria-hidden", "true");
					b.append(av, el("span", "chat-conv-name", p.nome));
					b.addEventListener("click", () => {
						search.value = "";
						results.hidden = true;
						openWith(p.utente);
					});
					li.append(b);
					results.append(li);
				});
			}
			results.hidden = false;
		}, 250);
	});

	// Emoji: griglia fissa, inserite nel punto del cursore.
	const EMOJI = ["😀", "😂", "😊", "🙂", "😉", "😍", "😎", "🤔", "😅", "😮", "😢", "😴", "😡", "🙄", "🤗", "🥳",
		"👍", "👎", "👏", "🙏", "💪", "👌", "✌️", "🤝", "👋", "❤️", "🎉", "🔥", "⭐", "✅", "❌", "⚠️",
		"☕", "🍕", "🎂", "📎", "📅", "📞", "💻", "📄"];
	const emojiBtn = $("[data-chat-emoji]");
	const emojiPanel = $("[data-chat-emoji-panel]");
	function emojiOpen(on) {
		emojiPanel.hidden = !on;
		emojiBtn.setAttribute("aria-expanded", String(on));
	}
	EMOJI.forEach((e) => {
		const b = el("button", "chat-emoji-item", e);
		b.type = "button";
		b.addEventListener("click", () => {
			const s = area.selectionStart ?? area.value.length, en = area.selectionEnd ?? s;
			area.setRangeText(e, s, en, "end");
			area.focus();
			updateCount();
		});
		emojiPanel.append(b);
	});
	emojiBtn.addEventListener("click", () => emojiOpen(emojiPanel.hidden));
	panel.addEventListener("keydown", (e) => { if (e.key === "Escape" && !emojiPanel.hidden) { e.stopPropagation(); e.preventDefault(); emojiOpen(false); emojiBtn.focus(); } });
	document.addEventListener("click", (e) => { if (!emojiPanel.hidden && !e.target.closest("[data-chat-emoji-panel], [data-chat-emoji]")) emojiOpen(false); });

	// Invio.
	function updateCount() {
		const n = [...area.value].length;
		count.textContent = n + "/" + MAX;
		count.classList.toggle("over", n > MAX);
	}
	area.addEventListener("input", updateCount);
	area.addEventListener("keydown", (e) => {
		if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
			e.preventDefault();
			form.requestSubmit();
		}
	});
	let sending = false;
	form.addEventListener("submit", async (e) => {
		e.preventDefault();
		if (sending || !current) return;
		const text = area.value.trim();
		if (!text) return;
		sending = true;
		err.hidden = true;
		const d = await api("POST", "/chat/messaggi", { a: current.con, testo: text });
		sending = false;
		if (!d.ok) {
			err.textContent = errText(d.errore);
			err.hidden = false;
			return;
		}
		area.value = "";
		emojiOpen(false);
		updateCount();
		if (verified) {
			reloadCurrent();
			loadList();
		} else {
			// Senza accesso il thread non si legge: si mostra solo ciò che si è appena scritto.
			msgs.append(messageItem({ mio: true, testo: text, verificato: false, quando: new Date().toISOString(), locale: true }));
			msgs.lastElementChild.scrollIntoView({ block: "end" });
		}
		say("Messaggio inviato.");
	});

	more.addEventListener("click", async () => {
		if (!current || !current.id || !oldest) return;
		const d = await api("GET", "/chat/conversazioni/" + current.id + "?prima=" + oldest);
		if (d.ok) renderThread(d, true);
	});

	if (blockBtn) blockBtn.addEventListener("click", async () => {
		if (!current) return;
		const on = !current.bloccato;
		if (on && !confirm("Bloccare " + current.nome + "? Non riceverai più i suoi messaggi e non lo saprà.")) return;
		const d = await api("POST", "/chat/blocchi", { utente: current.con, blocca: on ? "1" : "0" });
		if (!d.ok) { say(errText(d.errore)); return; }
		current.bloccato = on;
		blockBtn.textContent = on ? "Sblocca" : "Blocca";
		blockedNote.hidden = !on;
		say(on ? "Collega bloccato." : "Collega sbloccato.");
	});

	$("[data-chat-back]").addEventListener("click", () => show("list"));
	const close = $("[data-chat-close]");
	close.addEventListener("click", () => {
		if (dialog) dialog.close(); else location.href = "/";
	});

	// Accesso con la password di Windows (lo username è quello riconosciuto).
	const loginForm = $("[data-chat-login-form]");
	if (loginForm) loginForm.addEventListener("submit", async (e) => {
		e.preventDefault();
		const le = $("[data-chat-login-err]");
		le.hidden = true;
		const btn = loginForm.querySelector("button");
		btn.disabled = true;
		const d = await api("POST", "/chat/accesso", { password: loginForm.password.value });
		btn.disabled = false;
		loginForm.password.value = "";
		if (!d.ok) {
			le.textContent = errText(d.errore);
			le.hidden = false;
			return;
		}
		// Si ricarica per avere il pannello completo, tornando sulla stessa conversazione.
		try { sessionStorage.setItem(OPEN_KEY, current ? current.con : ""); } catch (_) {}
		location.reload();
	});
	const logout = $("[data-chat-logout]");
	if (logout) logout.addEventListener("click", async () => {
		await api("POST", "/chat/esci");
		location.reload();
	});

	document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") markRead(); });

	// Nuovi messaggi (evento SSE inoltrato da notifiche.js).
	document.body.addEventListener("chat", () => {
		if ((!dialog || dialog.open) && verified) {
			loadList();
			if (current) reloadCurrent();
		}
	});

	// Apertura iniziale: /chat?a=…, /chat?c=…, oppure ritorno dopo l'accesso.
	let reopen = null;
	try { reopen = sessionStorage.getItem(OPEN_KEY); sessionStorage.removeItem(OPEN_KEY); } catch (_) {}
	if (panel.dataset.open) openPanel(panel.dataset.open);
	else if (panel.dataset.openConv) { if (!dialog || dialog.open) loadList(); openConv(panel.dataset.openConv); }
	else if (reopen !== null) openPanel(reopen);
	else if (!dialog) { loadList(); show("list"); }
	updateCount();
})();
